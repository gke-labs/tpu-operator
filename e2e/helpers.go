package e2e

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func applyManifest(ctx context.Context, k8sClient client.Client, path string, obj client.Object) error {
	yamlBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	expandedYAML := expandManifest(yamlBytes)
	if err := yaml.Unmarshal(expandedYAML, obj); err != nil {
		return err
	}

	return k8sClient.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("e2e-test"))
}

// waitForCondition polls until the target object reaches the expected condition status.
// It assumes the object already exists. If the object is not found (NotFound), it returns
// a terminal error immediately to fail fast. Other transient errors during polling are logged and ignored.
func waitForCondition[T client.Object](
	ctx context.Context, k8sClient client.Client, key client.ObjectKey, obj T,
	getConditions func(T) []metav1.Condition, condType string, expectedStatus metav1.ConditionStatus, timeout time.Duration,
) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		if err := k8sClient.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return false, fmt.Errorf("resource %s was deleted while waiting for condition %s", key.Name, condType)
			}
			log.Printf("Ignoring transient error while waiting for condition %s on %s: %v", condType, key.Name, err)
			return false, nil // Ignore other transient errors
		}
		for _, c := range getConditions(obj) {
			if c.Type == condType && c.Status == expectedStatus {
				return true, nil
			}
		}
		return false, nil
	})
}

func waitForDeletion(ctx context.Context, k8sClient client.Client, key client.ObjectKey, obj client.Object, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		err := k8sClient.Get(ctx, key, obj)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, nil // Ignore errors
		}
		return false, nil
	})
}

func verifyTPUWorkload(t *testing.T, ctx context.Context, k8sClient client.Client, groupNamespace, groupName string, expectedNodes int) {
	var nodeList corev1.NodeList
	if err := k8sClient.List(ctx, &nodeList, client.MatchingLabels{
		"cloud.google.com/tpu-node-group-namespace": groupNamespace,
		"cloud.google.com/tpu-node-group-name":      groupName,
	}); err != nil {
		t.Fatalf("Failed to list nodes for TPU workload verification: %v", err)
	}
	if len(nodeList.Items) != expectedNodes {
		t.Fatalf("Expected %d nodes, found %d", expectedNodes, len(nodeList.Items))
	}

	// Sort nodes by name to ensure deterministic IP list
	sort.Slice(nodeList.Items, func(i, j int) bool {
		return nodeList.Items[i].Name < nodeList.Items[j].Name
	})

	nodeIPs := make([]string, expectedNodes)
	for i, node := range nodeList.Items {
		ip := ""
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				ip = addr.Address
				break
			}
		}
		if ip == "" {
			t.Fatalf("Failed to find InternalIP for node %s", node.Name)
		}
		nodeIPs[i] = ip
	}
	t.Logf("Found TPU Node IPs: %v", nodeIPs)

	jobName := "tpu-verify-" + groupNamespace + "-" + groupName
	completions := int32(expectedNodes)
	parallelism := int32(expectedNodes)

	// Create Headless Service for multi-host coordinator discovery
	if expectedNodes > 1 {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName + "-svc",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Selector: map[string]string{
					"job-name": jobName,
				},
				Ports: []corev1.ServicePort{
					{Name: "coordinator", Port: 1234},
					{Name: "runtime-1", Port: 8470},
					{Name: "runtime-2", Port: 8471},
				},
			},
		}
		t.Logf("Creating Headless Service %s-svc...", jobName)
		if err := k8sClient.Create(ctx, svc); err != nil {
			t.Fatalf("Failed to create Headless Service: %v", err)
		}
		t.Cleanup(func() {
			t.Logf("Cleaning up Headless Service %s-svc...", jobName)
			_ = k8sClient.Delete(context.Background(), svc)
		})
	}

	// Construct TPU env vars
	tpuProcessAddresses := ""
	tpuWorkerHostnames := ""
	for i, ip := range nodeIPs {
		if i > 0 {
			tpuProcessAddresses += ","
			tpuWorkerHostnames += ","
		}
		tpuProcessAddresses += fmt.Sprintf("%s:8471", ip)
		tpuWorkerHostnames += fmt.Sprintf("%s:8471", ip)
	}

	coordinatorAddress := fmt.Sprintf("%s:1234", nodeIPs[0])
	if expectedNodes > 1 {
		// Use Headless Service DNS for coordinator
		coordinatorAddress = fmt.Sprintf("%s-0.%s-svc.default.svc.cluster.local:1234", jobName, jobName)
	}

	topology := "2x2x1"
	if expectedNodes > 1 {
		topology = "2x2x2" // Assuming 2x2x2 for 2 nodes in multi-host E2E
	}

	// JAX command supporting multi-host
	jaxCommand := `
import os

# Map K8s Indexed Job index to JAX/TPU env vars BEFORE importing jax
if "JOB_COMPLETION_INDEX" in os.environ:
    os.environ["CLOUD_TPU_TASK_ID"] = os.environ["JOB_COMPLETION_INDEX"]
    os.environ["JAX_PROCESS_ID"] = os.environ["JOB_COMPLETION_INDEX"]
    os.environ["TPU_WORKER_ID"] = os.environ["JOB_COMPLETION_INDEX"]

import jax

expected_nodes = int(os.environ.get("JAX_PROCESS_COUNT", "1"))
if expected_nodes > 1:
    process_id = int(os.environ["JAX_PROCESS_ID"])
    coordinator_address = os.environ["JAX_COORDINATOR_ADDRESS"]
    print(f"Initializing JAX distributed: coordinator={coordinator_address}, total_processes={expected_nodes}, process_id={process_id}")
    jax.distributed.initialize(
        coordinator_address=coordinator_address,
        num_processes=expected_nodes,
        process_id=process_id,
    )

print("Devices:", jax.devices())
expected_devices = expected_nodes * 8
assert len(jax.devices()) == expected_devices, f"Expected {expected_devices} devices, got {len(jax.devices())}"
print("TPU OK")
`

	privileged := true

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
		},
		Spec: batchv1.JobSpec{
			Completions: &completions,
			Parallelism: &parallelism,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"job-name": jobName,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					HostNetwork:   true,
					DNSPolicy:     corev1.DNSClusterFirstWithHostNet,
					NodeSelector: map[string]string{
						"cloud.google.com/tpu-node-group-namespace": groupNamespace,
						"cloud.google.com/tpu-node-group-name":      groupName,
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "google.com/tpu",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "jax-tpu-verify",
							Image: "us-docker.pkg.dev/cloud-tpu-images/jax-ai-image/tpu@sha256:d3dbf1a00bf1c5b61bbd1d7456cca3ec090fd3200536a4b61a8fbd3de8ef23f3",
							Command: []string{
								"python3",
								"-c",
								jaxCommand,
							},
							Env: []corev1.EnvVar{
								{Name: "JAX_PROCESS_COUNT", Value: fmt.Sprintf("%d", expectedNodes)},
								{Name: "TPU_TOPOLOGY", Value: topology},
								{Name: "TPU_ACCELERATOR_TYPE", Value: "tpu7x-4"},
								{Name: "JAX_COORDINATOR_ADDRESS", Value: coordinatorAddress},
								{Name: "TPU_PROCESS_ADDRESSES", Value: tpuProcessAddresses},
								{Name: "TPU_WORKER_HOSTNAMES", Value: tpuWorkerHostnames},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"google.com/tpu": resource.MustParse("4"),
								},
								Requests: corev1.ResourceList{
									"google.com/tpu": resource.MustParse("4"),
								},
							},
						},
					},
				},
			},
		},
	}

	if expectedNodes > 1 {
		job.Spec.CompletionMode = func() *batchv1.CompletionMode {
			m := batchv1.IndexedCompletion
			return &m
		}()
		job.Spec.Template.Spec.Subdomain = jobName + "-svc"
	}

	t.Logf("Creating TPU Verification Job %s...", jobName)
	if err := k8sClient.Create(ctx, job); err != nil {
		t.Fatalf("Failed to create Job: %v", err)
	}
	t.Cleanup(func() {
		t.Logf("Cleaning up Job %s...", jobName)
		background := metav1.DeletePropagationBackground
		_ = k8sClient.Delete(context.Background(), job, &client.DeleteOptions{
			PropagationPolicy: &background,
		})
	})

	timeout := 5 * time.Minute
	if expectedNodes > 1 {
		timeout = 10 * time.Minute
	}

	t.Log("Waiting for Job to complete successfully...")
	if err := wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		actualJob := &batchv1.Job{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, actualJob); err != nil {
			return false, err
		}
		if actualJob.Status.Succeeded == completions {
			t.Log("Job completed successfully!")
			return true, nil
		}
		if actualJob.Status.Failed > 0 {
			return false, fmt.Errorf("job failed with %d failed pods", actualJob.Status.Failed)
		}
		t.Logf("Waiting for Job. Active=%d, Succeeded=%d, Failed=%d", actualJob.Status.Active, actualJob.Status.Succeeded, actualJob.Status.Failed)
		return false, nil
	}); err != nil {
		dumpPodLogs(t, ctx, k8sClient, jobName)
		t.Fatalf("Job did not complete successfully: %v", err)
	}
}

func dumpPodLogs(t *testing.T, ctx context.Context, k8sClient client.Client, jobName string) {
	var podList corev1.PodList
	if err := k8sClient.List(ctx, &podList, client.InNamespace("default"), client.MatchingLabels{"job-name": jobName}); err != nil {
		t.Logf("Failed to list pods for log dumping: %v", err)
		return
	}
	for _, pod := range podList.Items {
		t.Logf("--- Logs for Pod %s ---", pod.Name)
		cmd := exec.Command("kubectl", "logs", pod.Name, "-n", "default")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("Failed to get logs for %s: %v", pod.Name, err)
		} else {
			t.Log(string(out))
		}
	}
}

func verifyEvents(t *testing.T, ctx context.Context, kubeClient kubernetes.Interface, obj client.Object, expectedReasons []string) {
	t.Helper()
	t.Logf("Verifying events for %s/%s: %v", obj.GetNamespace(), obj.GetName(), expectedReasons)
	var foundReasons map[string]bool
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		events, err := kubeClient.CoreV1().Events(obj.GetNamespace()).List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s", obj.GetName()),
		})
		if err != nil {
			return false, err
		}

		foundReasons = make(map[string]bool)
		for _, event := range events.Items {
			foundReasons[event.Reason] = true
			t.Logf("Found event: Reason=%q, Message=%q", event.Reason, event.Message)
		}

		allFound := true
		for _, reason := range expectedReasons {
			if !foundReasons[reason] {
				allFound = false
			}
		}
		return allFound, nil
	})
	if err != nil {
		var missing []string
		for _, reason := range expectedReasons {
			if !foundReasons[reason] {
				missing = append(missing, reason)
			}
		}
		var found []string
		for r := range foundReasons {
			found = append(found, r)
		}
		t.Fatalf("verifyEvents(%s/%s) timed out: missing expected events %v; found %v; error: %v",
			obj.GetNamespace(), obj.GetName(), missing, found, err)
	}
	t.Log("All expected events found.")
}

// runGcloud executes a gcloud command with the provided args, returning the combined output.
// It logs the command and returns an error if execution fails.
func runGcloud(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Logf("Running: gcloud %s", strings.Join(args, " "))
	cmd := exec.Command("gcloud", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("gcloud error: %w, stderr: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// verifyGCEInstanceTemplateExists checks if a GCE instance template exists.
func verifyGCEInstanceTemplateExists(t *testing.T, project, name string, expectedExists bool) {
	t.Helper()
	args := []string{"compute", "instance-templates", "describe", name, "--project", project}
	_, err := runGcloud(t, args...)
	if expectedExists && err != nil {
		t.Fatalf("Expected GCE Instance Template %s to exist, but got error: %v", name, err)
	} else if !expectedExists && err == nil {
		t.Fatalf("Expected GCE Instance Template %s to NOT exist, but it still exists", name)
	}
}

// verifyGCEManagedInstanceGroupExists checks if a GCE MIG exists.
func verifyGCEManagedInstanceGroupExists(t *testing.T, project, zone, name string, expectedExists bool) {
	t.Helper()
	args := []string{"compute", "instance-groups", "managed", "describe", name, "--project", project, "--zone", zone}
	_, err := runGcloud(t, args...)
	if expectedExists && err != nil {
		t.Fatalf("Expected GCE Managed Instance Group %s to exist, but got error: %v", name, err)
	} else if !expectedExists && err == nil {
		t.Fatalf("Expected GCE Managed Instance Group %s to NOT exist, but it still exists", name)
	}
}

// verifyGCEResourcePolicyExists checks if a GCE Resource Policy exists.
func verifyGCEResourcePolicyExists(t *testing.T, project, region, name string, expectedExists bool) {
	t.Helper()
	args := []string{"compute", "resource-policies", "describe", name, "--project", project, "--region", region}
	_, err := runGcloud(t, args...)
	if expectedExists && err != nil {
		t.Fatalf("Expected GCE Resource Policy %s to exist, but got error: %v", name, err)
	} else if !expectedExists && err == nil {
		t.Fatalf("Expected GCE Resource Policy %s to NOT exist, but it still exists", name)
	}
}

func verifyTeardown(t *testing.T, ctx context.Context, k8sClient client.Client, ng *tpuapi.TPUNodeGroup, children ...client.Object) {
	t.Helper()
	t.Log("=== Teardown Verification ===")
	t.Logf("Deleting TPUNodeGroup CR %s...", ng.Name)
	ngKey := types.NamespacedName{Name: ng.GetName(), Namespace: ng.GetNamespace()}
	if err := k8sClient.Delete(ctx, ng); err != nil && !apierrors.IsNotFound(err) {
		t.Errorf("Failed to delete TPUNodeGroup CR: %v", err)
	}

	var wg sync.WaitGroup
	for _, child := range children {
		if child.GetName() == "" {
			t.Logf("Skipping cleanup for uninitialized child of type %T", child)
			continue
		}
		wg.Add(1)
		go func(c client.Object) {
			defer wg.Done()
			key := types.NamespacedName{Name: c.GetName(), Namespace: c.GetNamespace()}
			t.Logf("Verifying child %T %s deletion...", c, c.GetName())
			if err := waitForDeletion(ctx, k8sClient, key, c, 300*time.Second); err != nil {
				t.Errorf("Failed or timed out waiting for child %T deletion: %v", c, err)
			}
		}(child)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		t.Logf("Waiting for TPUNodeGroup CR %s deletion...", ng.Name)
		if err := waitForDeletion(ctx, k8sClient, ngKey, ng, 300*time.Second); err != nil {
			t.Errorf("Failed or timed out waiting for TPUNodeGroup CR deletion: %v", err)
		}
	}()

	wg.Wait()
}

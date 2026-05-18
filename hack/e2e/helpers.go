package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func applyManifest(ctx context.Context, k8sClient client.Client, path string, obj client.Object) error {
	yamlBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(yamlBytes, obj); err != nil {
		return err
	}
	return k8sClient.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("e2e-test"))
}

func waitForCondition[T client.Object](
	ctx context.Context, k8sClient client.Client, key client.ObjectKey, obj T,
	getConditions func(T) []metav1.Condition, condType string, expectedStatus metav1.ConditionStatus, timeout time.Duration,
) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		if err := k8sClient.Get(ctx, key, obj); err != nil {
			return false, err
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
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
}

func verifyTPUWorkload(t *testing.T, ctx context.Context, k8sClient client.Client, groupLabelValue string, expectedNodes int) {
	if expectedNodes != 1 {
		t.Fatalf("Phase 1 only supports expectedNodes == 1, got %d", expectedNodes)
	}

	var nodeList corev1.NodeList
	if err := k8sClient.List(ctx, &nodeList, client.MatchingLabels{
		"cloud.google.com/tpu-node-group": groupLabelValue,
	}); err != nil {
		t.Fatalf("Failed to list nodes for TPU workload verification: %v", err)
	}
	if len(nodeList.Items) != expectedNodes {
		t.Fatalf("Expected %d nodes, found %d", expectedNodes, len(nodeList.Items))
	}
	nodeIP := ""
	for _, addr := range nodeList.Items[0].Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			nodeIP = addr.Address
			break
		}
	}
	if nodeIP == "" {
		t.Fatalf("Failed to find InternalIP for node %s", nodeList.Items[0].Name)
	}
	t.Logf("Found TPU Node IP: %s", nodeIP)

	jobName := "tpu-verify-" + groupLabelValue
	completions := int32(1)
	parallelism := int32(1)

	// For single host, we just run local JAX check
	jaxCommand := `
import jax
print("Devices:", jax.devices())
expected_devices = 8
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
					NodeSelector: map[string]string{
						"cloud.google.com/tpu-node-group": groupLabelValue,
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
								{Name: "JAX_PROCESS_COUNT", Value: "1"},
								{Name: "JAX_PROCESS_ID", Value: "0"},
								{Name: "TPU_WORKER_ID", Value: "0"},
								{Name: "TPU_TOPOLOGY", Value: "2x2x1"},
								{Name: "TPU_ACCELERATOR_TYPE", Value: "tpu7x-4"},
								{Name: "JAX_COORDINATOR_ADDRESS", Value: fmt.Sprintf("%s:1234", nodeIP)},
								{Name: "TPU_PROCESS_ADDRESSES", Value: fmt.Sprintf("%s:8470", nodeIP)},
								{Name: "TPU_WORKER_HOSTNAMES", Value: fmt.Sprintf("%s:8471", nodeIP)},
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

	t.Log("Waiting for Job to complete successfully...")
	if err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
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

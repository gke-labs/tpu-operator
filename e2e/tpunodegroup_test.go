//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/controllers/tpunodegroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestTPUNodeGroup(t *testing.T) {
	cleanResources(t, []string{"tpunodegroups", "instancetemplates", "jobs", "nodes"})

	manifest := filepath.Join(repoRoot, "internal/controllers/tpunodegroup/testdata/test_nodegroup.yaml")
	crName := "test-nodegroup"
	dummyNodeGroup := &tpuapi.TPUNodeGroup{ObjectMeta: metav1.ObjectMeta{Name: crName}}
	childTemplateName := dummyNodeGroup.InstanceTemplateName()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	t.Log("=== Applying Test Manifest ===")
	ng := &tpuapi.TPUNodeGroup{}
	if err := applyManifest(ctx, k8sClient, manifest, ng); err != nil {
		t.Fatalf("Failed to apply manifest: %v", err)
	}

	itKey := types.NamespacedName{Name: childTemplateName, Namespace: "default"}
	it := &tpuapi.InstanceTemplate{}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cleanupCancel()
		verifyTeardown(t, cleanupCtx, k8sClient, ng, it)
	})

	t.Log("=== Waiting for child InstanceTemplate to be created ===")
	if err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		err := k8sClient.Get(ctx, itKey, it)
		if err == nil {
			return true, nil
		}
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}); err != nil {
		t.Fatalf("Timeout or error waiting for child InstanceTemplate to be created: %v", err)
	}
	t.Logf("Child InstanceTemplate %s created.", childTemplateName)

	t.Log("=== Verifying TPUNodeGroup Status ===")
	ngKey := types.NamespacedName{Name: crName, Namespace: "default"}
	if err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := k8sClient.Get(ctx, ngKey, ng); err != nil {
			return false, err
		}
		for _, c := range ng.Status.Conditions {
			if c.Type == "InstanceTemplateReady" && (c.Status == metav1.ConditionTrue || c.Status == metav1.ConditionFalse) {
				t.Logf("TPUNodeGroup InstanceTemplateReady status: %s, reason: %s", c.Status, c.Reason)
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("InstanceTemplateReady condition not found or invalid: %v", err)
	}

	t.Log("=== Verifying WorkloadPolicy Status ===")
	wpKey := types.NamespacedName{Name: crName, Namespace: "default"}
	wp := &tpuapi.WorkloadPolicy{}
	err := k8sClient.Get(ctx, wpKey, wp)
	if err == nil {
		t.Fatal("WorkloadPolicy should not have been created for single-host slice")
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("Expected NotFound error, got: %v", err)
	}
	t.Log("Verified WorkloadPolicy was not created.")

	t.Log("=== Verifying Node Joining and Labeling ===")
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := k8sClient.Get(ctx, ngKey, ng); err != nil {
			return false, err
		}
		if ng.Status.NodeSummary != nil && ng.Status.NodeSummary.Ready == 1 {
			t.Log("NodeSummary indicates 1 Ready node.")
			return true, nil
		}
		if ng.Status.NodeSummary != nil {
			t.Logf("Waiting for nodes to join. NodeSummary: Ready=%d, Reconciling=%d", ng.Status.NodeSummary.Ready, ng.Status.NodeSummary.Reconciling)
		} else {
			t.Log("Waiting for NodeSummary to be populated...")
		}
		return false, nil
	}); err != nil {
		t.Fatalf("Timeout or error waiting for node to join: %v", err)
	}

	var nodeList corev1.NodeList
	if err := k8sClient.List(ctx, &nodeList, client.MatchingLabels{
		"cloud.google.com/tpu-node-group-namespace": "default",
		"cloud.google.com/tpu-node-group-name":      "test-nodegroup",
	}); err != nil {
		t.Fatalf("Failed to list nodes by TPUNodeGroup label: %v", err)
	}
	if len(nodeList.Items) != 1 {
		t.Fatalf("Expected exactly 1 node with TPUNodeGroup label, found %d", len(nodeList.Items))
	}
	node := nodeList.Items[0]
	t.Logf("Found node: %s", node.Name)

	ready := false
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("Node %s is not Ready", node.Name)
	}

	expectedLabels := map[string]string{
		"cloud.google.com/gke-tpu-accelerator":   "tpu7x",
		"cloud.google.com/gke-accelerator-count": "4",
	}
	for k, expectedVal := range expectedLabels {
		val, ok := node.Labels[k]
		if !ok || val != expectedVal {
			t.Fatalf("Node %s label mismatch for %s. Expected %s, got %s", node.Name, k, expectedVal, val)
		}
		t.Logf("Node %s label verified: %s=%s", node.Name, k, val)
	}

	t.Log("=== Verifying TPUNodeGroup Ready Condition ===")
	if err := waitForCondition(ctx, k8sClient, ngKey, ng, func(obj *tpuapi.TPUNodeGroup) []metav1.Condition {
		return obj.Status.Conditions
	}, "Ready", metav1.ConditionTrue, 2*time.Minute); err != nil {
		t.Fatalf("Timeout waiting for TPUNodeGroup CR to be Ready: %v", err)
	}

	t.Log("=== Verifying TPU Workload (Single-Host) ===")
	verifyTPUWorkload(t, ctx, k8sClient, "default", "test-nodegroup", 1)
}

func TestTPUNodeGroup_MultiHost(t *testing.T) {
	cleanResources(t, []string{"tpunodegroups", "instancetemplates", "workloadpolicies", "managedinstancegroups", "jobs", "nodes"})

	manifest := filepath.Join(repoRoot, "internal/controllers/tpunodegroup/testdata/test_nodegroup_multi_host.yaml")
	crName := "test-multihost"
	dummyNodeGroup := &tpuapi.TPUNodeGroup{ObjectMeta: metav1.ObjectMeta{Name: crName}}
	childTemplateName := dummyNodeGroup.InstanceTemplateName()
	childPolicyName := dummyNodeGroup.WorkloadPolicyName()
	migName := dummyNodeGroup.ManagedInstanceGroupName()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	ng := &tpuapi.TPUNodeGroup{}
	it := &tpuapi.InstanceTemplate{}
	wp := &tpuapi.WorkloadPolicy{}
	mig := &tpuapi.ManagedInstanceGroup{}

	ngKey := types.NamespacedName{Name: crName, Namespace: "default"}
	itKey := types.NamespacedName{Name: childTemplateName, Namespace: "default"}
	wpKey := types.NamespacedName{Name: childPolicyName, Namespace: "default"}
	migKey := types.NamespacedName{Name: migName, Namespace: "default"}

	t.Log("=== Applying Test Manifest ===")
	if err := applyManifest(ctx, k8sClient, manifest, ng); err != nil {
		t.Fatalf("Failed to apply manifest: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cleanupCancel()
		verifyTeardown(t, cleanupCtx, k8sClient, ng, mig, it, wp)
	})

	t.Run("TPUNodeGroup_Orchestration", func(t *testing.T) {
		t.Log("=== Verifying Finalizers ===")
		expectedFinalizers := []string{
			"tpu.google.com/cleanup-mig",
			"tpu.google.com/cleanup-template",
			"tpu.google.com/cleanup-policy",
		}
		if err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := k8sClient.Get(ctx, ngKey, ng); err != nil {
				return false, err
			}
			finalizers := ng.GetFinalizers()
			foundCount := 0
			for _, ef := range expectedFinalizers {
				for _, f := range finalizers {
					if f == ef {
						foundCount++
						break
					}
				}
			}
			if foundCount == len(expectedFinalizers) {
				t.Log("All expected finalizers found.")
				return true, nil
			}
			return false, nil
		}); err != nil {
			t.Fatalf("Timeout or error waiting for finalizers to be set: %v, current finalizers: %v", err, ng.GetFinalizers())
		}

		t.Log("=== Waiting for child WorkloadPolicy to have URI ===")
		if err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := k8sClient.Get(ctx, wpKey, wp); err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			if len(wp.Status.URI) > 0 {
				t.Logf("Child WorkloadPolicy %s has URI: %s", childPolicyName, wp.Status.URI)
				return true, nil
			}
			return false, nil
		}); err != nil {
			t.Fatalf("Timeout or error waiting for child WorkloadPolicy to have URI: %v", err)
		}

		t.Log("=== Waiting for child InstanceTemplate to be created ===")
		if err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
			err := k8sClient.Get(ctx, itKey, it)
			if err == nil {
				return true, nil
			}
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}); err != nil {
			t.Fatalf("Timeout or error waiting for child InstanceTemplate to be created: %v", err)
		}
		t.Logf("Child InstanceTemplate %s created.", childTemplateName)
	})

	t.Run("ManagedInstanceGroup_Provisioning", func(t *testing.T) {
		t.Log("=== Waiting for ManagedInstanceGroup to be created by controller ===")
		if err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
			err := k8sClient.Get(ctx, migKey, mig)
			if err == nil {
				return true, nil
			}
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}); err != nil {
			t.Fatalf("Timeout or error waiting for ManagedInstanceGroup to be created: %v", err)
		}
		t.Logf("ManagedInstanceGroup %s created.", migName)

		t.Log("=== Waiting for ManagedInstanceGroup to be ready ===")
		if err := waitForCondition(ctx, k8sClient, migKey, mig, func(obj *tpuapi.ManagedInstanceGroup) []metav1.Condition {
			return obj.Status.Conditions
		}, "Ready", metav1.ConditionTrue, 1200*time.Second); err != nil {
			_ = k8sClient.Get(ctx, migKey, mig)
			t.Logf("Timeout waiting for ManagedInstanceGroup to be ready. Dumping status: %+#v", mig)
			t.Fatalf("Timeout waiting for ManagedInstanceGroup to be ready: %v", err)
		}
		t.Logf("ManagedInstanceGroup %s is ready.", migName)

		t.Log("=== Verifying GCP resource creation ===")
		project := os.Getenv("E2E_PROJECT")
		if project == "" {
			t.Fatal("E2E_PROJECT environment variable must be set")
		}
		zone := os.Getenv("E2E_ZONE")
		if zone == "" {
			zone = "us-central1-c"
		}
		verifyGCEManagedInstanceGroupExists(t, project, zone, migName, true)

		t.Log("=== Verifying Node Joining and Labeling (Multi-Host) ===")
		if err := wait.PollUntilContextTimeout(ctx, 10*time.Second, 15*time.Minute, true, func(ctx context.Context) (bool, error) {
			if err := k8sClient.Get(ctx, ngKey, ng); err != nil {
				return false, nil // Ignore errors during polling
			}
			if ng.Status.NodeSummary != nil && ng.Status.NodeSummary.Ready == 2 {
				t.Log("NodeSummary indicates 2 Ready nodes.")
				return true, nil
			}
			if ng.Status.NodeSummary != nil {
				t.Logf("Waiting for nodes to join. NodeSummary: Ready=%d, Reconciling=%d", ng.Status.NodeSummary.Ready, ng.Status.NodeSummary.Reconciling)
			} else {
				t.Log("Waiting for NodeSummary to be populated...")
			}
			return false, nil
		}); err != nil {
			t.Fatalf("Timeout or error waiting for nodes to join: %v", err)
		}

		var nodeList corev1.NodeList
		if err := k8sClient.List(ctx, &nodeList, client.MatchingLabels{
			"cloud.google.com/tpu-node-group-namespace": "default",
			"cloud.google.com/tpu-node-group-name":      "test-multihost",
		}); err != nil {
			t.Fatalf("Failed to list nodes by TPUNodeGroup label: %v", err)
		}
		if len(nodeList.Items) != 2 {
			t.Fatalf("Expected exactly 2 nodes with TPUNodeGroup label, found %d", len(nodeList.Items))
		}

		for _, node := range nodeList.Items {
			ready := false
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if !ready {
				t.Fatalf("Node %s is not Ready", node.Name)
			}

			expectedLabels := map[string]string{
				"cloud.google.com/gke-tpu-accelerator":   "tpu7x",
				"cloud.google.com/gke-accelerator-count": "4",
				"cloud.google.com/gke-tpu-topology":      "2x2x2",
			}
			for k, expectedVal := range expectedLabels {
				val, ok := node.Labels[k]
				if !ok || val != expectedVal {
					t.Fatalf("Node %s label mismatch for %s. Expected %s, got %s", node.Name, k, expectedVal, val)
				}
				t.Logf("Node %s label verified: %s=%s", node.Name, k, val)
			}
		}

		t.Log("=== Verifying TPUNodeGroup Ready Condition (Multi-Host) ===")
		if err := waitForCondition(ctx, k8sClient, ngKey, ng, func(obj *tpuapi.TPUNodeGroup) []metav1.Condition {
			return obj.Status.Conditions
		}, "Ready", metav1.ConditionTrue, 2*time.Minute); err != nil {
			t.Fatalf("Timeout waiting for TPUNodeGroup CR to be Ready: %v", err)
		}

		t.Log("=== Verifying TPU Workloads (Multi-Host) ===")
		verifyTPUWorkload(t, ctx, k8sClient, "default", "test-multihost", 2)

		t.Log("=== Verifying Controller Events ===")
		verifyEvents(t, ctx, kubernetesClient, ng, []string{"ChildResourcesProvisioned", "NodesJoining", "Provisioned"})
	})
}

func TestTPUNodeGroup_BYOInstanceTemplate(t *testing.T) {
	cleanResources(t, []string{"tpunodegroups", "instancetemplates", "managedinstancegroups", "nodes", "jobs"})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	project := Config.Project
	zone := Config.Zone
	region := Config.Region

	crName := "test-byo-nodegroup"
	dummyNodeGroup := &tpuapi.TPUNodeGroup{ObjectMeta: metav1.ObjectMeta{Name: crName}}
	childTemplateName := dummyNodeGroup.InstanceTemplateName()
	templateName := "e2e-byo-template-" + strings.ToLower(rand.String(6))

	ngKey := types.NamespacedName{Name: crName, Namespace: "default"}

	templateURI := fmt.Sprintf("projects/%s/global/instanceTemplates/%s", project, templateName)
	ng := &tpuapi.TPUNodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: "default",
		},
		Spec: tpuapi.TPUNodeGroupSpec{
			Project:                   project,
			NodeLocation:              zone,
			NodeCount:                 1,
			Topology:                  "2x2x1",
			TargetSizePolicyMode:      "INDIVIDUAL",
			AcceleratorConnectionMode: "STATIC",
			InstanceTemplateURI:       &templateURI,
		},
	}
	mig := &tpuapi.ManagedInstanceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyNodeGroup.ManagedInstanceGroupName(),
			Namespace: "default",
		},
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cleanupCancel()
		verifyTeardown(t, cleanupCtx, k8sClient, ng, mig)
		t.Logf("=== Cleaning up External Instance Template %s ===", templateName)
		_, _ = runGcloud(t, "compute", "instance-templates", "delete", templateName, "--project", project, "--quiet")
	})

	t.Run("CreateGCEInstanceTemplate", func(t *testing.T) {
		cpIP := Config.ControlPlaneIP

		// 2. Generate Bootstrap Token and CA Hash
		t.Log("=== Generating Bootstrap Token and CA Hash ===")
		token, err := tpunodegroup.GenerateBootstrapToken(ctx, k8sClient, nil)
		if err != nil {
			t.Fatalf("Failed to generate bootstrap token: %v", err)
		}
		caHash, err := tpunodegroup.FetchCAHash(ctx, k8sClient)
		if err != nil {
			t.Fatalf("Failed to fetch CA hash: %v", err)
		}

		// 3. Create GCE Instance Template using gcloud
		t.Logf("=== Creating External Instance Template %s via gcloud ===", templateName)

		scriptContent := tpunodegroup.RenderStartupScript("1.31", project, zone)

		tmpScript, err := os.CreateTemp("", "startup-*.sh")
		if err != nil {
			t.Fatalf("Failed to create temp script: %v", err)
		}
		defer os.Remove(tmpScript.Name())
		if _, err := tmpScript.WriteString(scriptContent); err != nil {
			t.Fatalf("Failed to write to temp script: %v", err)
		}
		tmpScript.Close()

		args := []string{
			"compute", "instance-templates", "create", templateName,
			"--project", project,
			"--machine-type", "tpu7x-standard-4t",
			"--image", "projects/ubuntu-os-accelerator-images/global/images/family/ubuntu-accel-2404-amd64-tpu-tpu7x",
			"--boot-disk-size", "250GB",
			"--maintenance-policy", "TERMINATE",
			"--subnet", fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/subnetworks/default", project, region),
			"--no-address",
			fmt.Sprintf("--metadata-from-file=startup-script=%s", tmpScript.Name()),
			fmt.Sprintf("--metadata=kubeadm-join-token=%s,kubeadm-control-plane-ip=%s,kubeadm-ca-hash=%s", token, cpIP, caHash),
		}

		reservation := Config.Reservation
		if reservation != "" {
			args = append(args, "--reservation-affinity=specific", fmt.Sprintf("--reservation=%s", reservation), "--provisioning-model=RESERVATION_BOUND", "--instance-termination-action=DELETE")
		} else {
			args = append(args, "--provisioning-model=SPOT", "--instance-termination-action=STOP")
		}

		_, err = runGcloud(t, args...)
		if err != nil {
			t.Fatalf("Failed to create instance template via gcloud: %v", err)
		}
		t.Log("External Instance Template created successfully.")
	})

	t.Run("TPUNodeGroup_Orchestration", func(t *testing.T) {
		t.Log("=== Creating TPUNodeGroup with BYO template ===")
		if err := k8sClient.Create(ctx, ng); err != nil {
			t.Fatalf("Failed to create TPUNodeGroup: %v", err)
		}

		// Verify InstanceTemplateReady condition
		t.Log("=== Waiting for TPUNodeGroup to recognize ExternalTemplate ===")
		if err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := k8sClient.Get(ctx, ngKey, ng); err != nil {
				return false, err
			}
			for _, c := range ng.Status.Conditions {
				if c.Type == "InstanceTemplateReady" && c.Status == metav1.ConditionTrue {
					if c.Reason == "ExternalTemplate" {
						return true, nil
					}
					t.Fatalf("Expected Reason ExternalTemplate, got %s", c.Reason)
				}
			}
			return false, nil
		}); err != nil {
			t.Fatalf("Timeout or error waiting for TPUNodeGroup InstanceTemplateReady condition: %v", err)
		}

		t.Log("=== Verifying child InstanceTemplate is NOT created ===")
		childIt := &tpuapi.InstanceTemplate{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: childTemplateName, Namespace: "default"}, childIt); err == nil {
			t.Fatalf("Child InstanceTemplate should not have been created, but found: %s", childTemplateName)
		}
	})

	t.Run("ManagedInstanceGroup_Provisioning", func(t *testing.T) {
		t.Log("=== Waiting for TPUNodeGroup to be Ready (Nodes joined) ===")
		if err := wait.PollUntilContextTimeout(ctx, 10*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
			if err := k8sClient.Get(ctx, ngKey, ng); err != nil {
				return false, err
			}
			if ng.Status.NodeSummary != nil && ng.Status.NodeSummary.Ready == 1 {
				return true, nil
			}
			return false, nil
		}); err != nil {
			t.Fatalf("Timeout waiting for node to join: %v", err)
		}

		t.Log("=== Verifying TPU Workload (BYO) ===")
		verifyTPUWorkload(t, ctx, k8sClient, "default", "test-byo-nodegroup", 1)
	})
}

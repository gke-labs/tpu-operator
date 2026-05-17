package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestInstanceTemplate(t *testing.T) {
	// Clean resources
	cleanResources(t, []string{"instancetemplates"})

	manifest := filepath.Join(repoRoot, "pkg/controllers/instancetemplate/testdata/test_template.yaml")
	crName := "tpu-node-group-test-template"
	project := os.Getenv("E2E_PROJECT")
	if project == "" {
		project = "gsc-nexus-xteam-shared-testing"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	t.Log("=== Applying Test Manifest ===")
	it := &v1alpha1.InstanceTemplate{}
	if err := applyManifest(ctx, k8sClient, manifest, it); err != nil {
		t.Fatalf("Failed to apply manifest: %v", err)
	}

	t.Log("=== Waiting for InstanceTemplate to become Ready ===")
	key := types.NamespacedName{Name: crName, Namespace: "default"}
	err := waitForCondition(ctx, k8sClient, key, it, func(obj *v1alpha1.InstanceTemplate) []metav1.Condition {
		return obj.Status.Conditions
	}, "Ready", metav1.ConditionTrue, 120*time.Second)

	if err != nil {
		t.Fatalf("Timeout or error waiting for InstanceTemplate to become Ready: %v", err)
	}

	// Fetch updated to get URI
	if err := k8sClient.Get(ctx, key, it); err != nil {
		t.Fatalf("Failed to get updated InstanceTemplate: %v", err)
	}
	if len(it.Status.URI) == 0 {
		t.Fatal("InstanceTemplate is Ready but URI is empty")
	}
	t.Logf("InstanceTemplate is Ready. URI: %s", it.Status.URI)

	t.Log("=== Verifying GCP Resource Creation ===")
	cmd := exec.Command("gcloud", "compute", "instance-templates", "describe", crName, "--project", project)
	if err := cmd.Run(); err != nil {
		t.Fatalf("GCP Instance Template not found or error: %v", err)
	}
	t.Log("GCP resource verified.")

	t.Log("=== Teardown Verification ===")
	t.Log("Deleting InstanceTemplate CR...")
	if err := k8sClient.Delete(ctx, it); err != nil {
		t.Fatalf("Failed to delete InstanceTemplate CR: %v", err)
	}

	t.Log("Waiting for CR deletion...")
	if err := waitForDeletion(ctx, k8sClient, key, it, 300*time.Second); err != nil {
		t.Fatalf("Failed or timed out waiting for CR deletion: %v", err)
	}

	t.Log("Verifying GCP resource deletion...")
	cmd = exec.Command("gcloud", "compute", "instance-templates", "describe", crName, "--project", project)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("GCP Instance Template still exists after CR deletion!")
	}
	t.Log("GCP Instance Template deleted successfully.")
}


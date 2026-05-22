//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gke-labs/tpu-operator/pkg/apis/tpu/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestWorkloadPolicy(t *testing.T) {
	// Clean resources
	cleanResources(t, []string{"workloadpolicies"})

	manifest := filepath.Join(repoRoot, "pkg/controllers/workloadpolicy/testdata/test_workloadpolicy.yaml")
	crName := "test-workloadpolicy"
	project := os.Getenv("E2E_PROJECT")
	if project == "" {
		t.Fatal("E2E_PROJECT environment variable must be set")
	}
	region := os.Getenv("E2E_REGION")
	if region == "" {
		region = "us-central1"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	t.Log("=== Applying Test Manifest ===")
	wp := &v1alpha1.WorkloadPolicy{}
	if err := applyManifest(ctx, k8sClient, manifest, wp); err != nil {
		t.Fatalf("Failed to apply manifest: %v", err)
	}

	t.Log("=== Waiting for WorkloadPolicy to become Ready ===")
	key := types.NamespacedName{Name: crName, Namespace: "default"}
	err := waitForCondition(ctx, k8sClient, key, wp, func(obj *v1alpha1.WorkloadPolicy) []metav1.Condition {
		return obj.Status.Conditions
	}, "Ready", metav1.ConditionTrue, 120*time.Second)

	if err != nil {
		t.Fatalf("Timeout or error waiting for WorkloadPolicy to become Ready: %v", err)
	}

	// Fetch updated to get URI
	if err := k8sClient.Get(ctx, key, wp); err != nil {
		t.Fatalf("Failed to get updated WorkloadPolicy: %v", err)
	}
	if len(wp.Status.URI) == 0 {
		t.Fatal("WorkloadPolicy is Ready but URI is empty")
	}
	t.Logf("WorkloadPolicy is Ready. URI: %s", wp.Status.URI)

	t.Log("=== Verifying GCP Resource Creation ===")
	cmd := exec.Command("gcloud", "compute", "resource-policies", "describe", crName, "--project", project, "--region", region)
	if err := cmd.Run(); err != nil {
		t.Fatalf("GCP Resource Policy not found or error: %v", err)
	}
	t.Log("GCP resource verified.")

	t.Log("=== Teardown Verification ===")
	t.Log("Deleting WorkloadPolicy CR...")
	if err := k8sClient.Delete(ctx, wp); err != nil {
		t.Fatalf("Failed to delete WorkloadPolicy CR: %v", err)
	}

	t.Log("Waiting for CR deletion...")
	if err := waitForDeletion(ctx, k8sClient, key, wp, 300*time.Second); err != nil {
		t.Fatalf("Failed or timed out waiting for CR deletion: %v", err)
	}

	t.Log("Verifying GCP resource deletion...")
	cmd = exec.Command("gcloud", "compute", "resource-policies", "describe", crName, "--project", project, "--region", region)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("GCP Resource Policy still exists after CR deletion!")
	}
	t.Log("GCP Resource Policy deleted successfully.")
}


package e2e

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkloadPolicy(t *testing.T) {
	// Clean resources
	cleanResources(t, []string{"workloadpolicies"})

	manifest := filepath.Join(repoRoot, "pkg/controllers/workloadpolicy/testdata/test_workloadpolicy.yaml")
	crName := "test-workloadpolicy"
	project := "gsc-nexus-xteam-shared-testing"
	region := "us-central1"

	t.Log("=== Applying Test Manifest ===")
	cmd := exec.Command("kubectl", "apply", "-f", manifest, "--request-timeout=30s")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to apply manifest: %v", err)
	}

	t.Log("=== Waiting for WorkloadPolicy to become Ready ===")
	timeout := 120 * time.Second
	interval := 5 * time.Second
	start := time.Now()

	for {
		if time.Since(start) > timeout {
			t.Fatal("Timeout waiting for WorkloadPolicy to become Ready")
		}

		// Check ready status
		cmd = exec.Command("kubectl", "get", "workloadpolicy", crName, "-o", "jsonpath={.status.conditions[?(@.type==\"Ready\")].status}", "--request-timeout=30s")
		ready, _ := cmd.Output()

		cmd = exec.Command("kubectl", "get", "workloadpolicy", crName, "-o", "jsonpath={.status.uri}", "--request-timeout=30s")
		uri, _ := cmd.Output()

		if string(ready) == "True" && len(uri) > 0 {
			t.Logf("WorkloadPolicy is Ready. URI: %s", string(uri))
			break
		}

		time.Sleep(interval)
	}

	t.Log("=== Verifying GCP Resource Creation ===")
	cmd = exec.Command("gcloud", "compute", "resource-policies", "describe", crName, "--project", project, "--region", region)
	if err := cmd.Run(); err != nil {
		t.Fatalf("GCP Resource Policy not found or error: %v", err)
	}
	t.Log("GCP resource verified.")

	t.Log("=== Teardown Verification ===")
	t.Log("Deleting WorkloadPolicy CR...")
	cmd = exec.Command("kubectl", "delete", "workloadpolicy", crName, "--timeout=300s", "--request-timeout=30s")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to delete WorkloadPolicy CR: %v\nStdout: %s\nStderr: %s", err, stdout.String(), stderr.String())
	}

	t.Log("Verifying GCP resource deletion...")
	cmd = exec.Command("gcloud", "compute", "resource-policies", "describe", crName, "--project", project, "--region", region)
	stderr.Reset()
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("GCP Resource Policy still exists after CR deletion!")
	}
	t.Log("GCP Resource Policy deleted successfully.")
}

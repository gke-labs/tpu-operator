package e2e

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTPUNodeGroup(t *testing.T) {
	// Clean resources
	cleanResources(t, []string{"tpunodegroups", "instancetemplates"})

	manifest := filepath.Join(repoRoot, "pkg/controllers/tpunodegroup/testdata/test_nodegroup.yaml")
	crName := "test-nodegroup"
	childTemplateName := crName + "-template"

	t.Log("=== Applying Test Manifest ===")
	cmd := exec.Command("kubectl", "apply", "-f", manifest, "--request-timeout=30s")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to apply manifest: %v", err)
	}

	t.Log("=== Waiting for child InstanceTemplate to be created ===")
	timeout := 120 * time.Second
	interval := 5 * time.Second
	start := time.Now()

	for {
		if time.Since(start) > timeout {
			t.Fatal("Timeout waiting for child InstanceTemplate to be created")
		}

		cmd = exec.Command("kubectl", "get", "instancetemplate", childTemplateName, "--request-timeout=30s")
		if err := cmd.Run(); err == nil {
			t.Logf("Child InstanceTemplate %s created.", childTemplateName)
			break
		}

		time.Sleep(interval)
	}

	t.Log("=== Verifying TPUNodeGroup Status ===")
	cmd = exec.Command("kubectl", "get", "tpunodegroup", crName, "-o", "jsonpath={.status.conditions[?(@.type==\"InstanceTemplateReady\")].status}", "--request-timeout=30s")
	status, _ := cmd.Output()

	cmd = exec.Command("kubectl", "get", "tpunodegroup", crName, "-o", "jsonpath={.status.conditions[?(@.type==\"InstanceTemplateReady\")].reason}", "--request-timeout=30s")
	reason, _ := cmd.Output()

	t.Logf("TPUNodeGroup InstanceTemplateReady status: %s, reason: %s", string(status), string(reason))

	if string(status) != "True" && string(status) != "False" {
		t.Fatal("InstanceTemplateReady condition not found or invalid.")
	}

	t.Log("=== Teardown Verification ===")
	t.Log("Deleting TPUNodeGroup CR...")
	cmd = exec.Command("kubectl", "delete", "tpunodegroup", crName, "--timeout=300s", "--request-timeout=30s")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to delete TPUNodeGroup CR: %v\nStdout: %s\nStderr: %s", err, stdout.String(), stderr.String())
	}

	t.Log("Verifying child InstanceTemplate deletion...")
	timeout = 60 * time.Second
	start = time.Now()
	for {
		if time.Since(start) > timeout {
			t.Fatal("Timeout waiting for child InstanceTemplate to be deleted")
		}

		cmd = exec.Command("kubectl", "get", "instancetemplate", childTemplateName, "--request-timeout=30s")
		if err := cmd.Run(); err != nil {
			t.Log("Child InstanceTemplate deleted successfully.")
			break
		}

		time.Sleep(interval)
	}
}

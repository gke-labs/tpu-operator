package e2e

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

var controllerCmd *exec.Cmd
var logFile *os.File
var repoRoot string

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() {
	fmt.Println("=== Global Setup ===")

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: %v", err)
	}
	// Assuming we are in hack/e2e, repo root is two levels up.
	repoRoot = filepath.Dir(filepath.Dir(wd))

	fmt.Println("=== Refreshing CRDs ===")
	cmd := exec.Command("make", "manifests")
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to run make manifests: %v", err)
	}

	cmd = exec.Command("kubectl", "apply", "-f", "deploy/crds/", "--request-timeout=30s")
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to apply CRDs: %v", err)
	}

	fmt.Println("=== Starting Controller ===")
	logPath := "/tmp/controller_e2e.log"
	logFile, err = os.Create(logPath)
	if err != nil {
		log.Fatalf("Failed to create log file: %v", err)
	}

	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")

	controllerCmd = exec.Command("go", "run", "cmd/main.go", "--kube-config", kubeconfig)
	controllerCmd.Dir = repoRoot
	controllerCmd.Stdout = logFile
	controllerCmd.Stderr = logFile

	if err := controllerCmd.Start(); err != nil {
		log.Fatalf("Failed to start controller: %v", err)
	}

	fmt.Printf("Controller started with PID: %d, logs at %s\n", controllerCmd.Process.Pid, logPath)

	// Give controller a moment to start
	time.Sleep(3 * time.Second)
}

func teardown() {
	fmt.Println("=== Global Teardown ===")
	if controllerCmd != nil && controllerCmd.Process != nil {
		fmt.Printf("Terminating controller process (PID: %d)...\n", controllerCmd.Process.Pid)
		if err := controllerCmd.Process.Kill(); err != nil {
			fmt.Printf("Failed to kill controller process: %v\n", err)
		}
	}
	if logFile != nil {
		logFile.Close()
	}
}

func cleanResources(t *testing.T, resourceTypes []string) {
	t.Helper()
	for _, rt := range resourceTypes {
		// Check stuck resources
		cmd := exec.Command("kubectl", "get", rt, "-o", "jsonpath={.items[?(@.metadata.deletionTimestamp)].metadata.name}")
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("Failed to check stuck resources for %s: %v", rt, err)
		}
		if len(output) > 0 {
			t.Fatalf("ERROR: Found %s stuck in deletion: %s", rt, string(output))
		}

		// Delete all
		cmd = exec.Command("kubectl", "delete", rt, "--all", "--ignore-not-found", "--timeout=60s", "--request-timeout=30s")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to delete resources of type %s: %v", rt, err)
		}
	}
}

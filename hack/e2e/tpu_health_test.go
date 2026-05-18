//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRemoteClusterTPUHealth(t *testing.T) {
	t.Log("=== Verifying TPU Health on Remote GCE Cluster ===")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. Verify Nodes are Ready and TPU Capacity on Worker
	nodeList := &corev1.NodeList{}
	if err := k8sClient.List(ctx, nodeList); err != nil {
		t.Fatalf("Failed to list nodes: %v", err)
	}

	foundWorker := false
	for _, node := range nodeList.Items {
		ready := false
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}

		if !ready {
			t.Errorf("Node %s is NOT Ready", node.Name)
		}

		if strings.Contains(node.Name, "worker-1") {
			foundWorker = true
			tpuCapacity, ok := node.Status.Allocatable["google.com/tpu"]
			if !ok {
				t.Errorf("Node %s is missing google.com/tpu capacity", node.Name)
			} else {
				t.Logf("Node %s has google.com/tpu: %s", node.Name, tpuCapacity.String())
				if tpuCapacity.String() != "4" {
					t.Errorf("Expected 4 TPU chips, got %s", tpuCapacity.String())
				}
			}
		}
	}

	if !foundWorker {
		t.Error("Did not find expected worker node (worker-1)")
	}

	// 2. Verify TPU Device Plugin is running
	podList := &corev1.PodList{}
	if err := k8sClient.List(ctx, podList, client.InNamespace("kube-system"), client.MatchingLabels{"name": "tpu-device-plugin"}); err != nil {
		t.Errorf("Failed to list tpu-device-plugin pods: %v", err)
	} else {
		if len(podList.Items) == 0 {
			t.Error("No tpu-device-plugin pods found in kube-system namespace")
		}
		for _, pod := range podList.Items {
			if pod.Status.Phase != corev1.PodRunning {
				t.Errorf("tpu-device-plugin pod %s is not Running. Phase: %s", pod.Name, pod.Status.Phase)
			} else {
				t.Logf("tpu-device-plugin pod %s is Running.", pod.Name)
			}
		}
	}

	// 3. Check JAX logs if tpu-worker-0 exists
	cmd := exec.Command("kubectl", "logs", "tpu-worker-0", "--tail=20")
	output, err := cmd.Output()
	if err == nil {
		t.Log("JAX logs from tpu-worker-0:")
		fmt.Println(string(output))
		outStr := string(output)
		if strings.Contains(outStr, "TPU cores: 8") || strings.Contains(outStr, "Detected 8 TPU cores") || strings.Contains(outStr, "cores in the slice: 8") {
			t.Log("Verified JAX detects 8 TPU cores.")
		} else {
			t.Errorf("JAX logs do not confirm 8 TPU cores detection. Snippet: %s", outStr)
		}
	} else {
		t.Log("tpu-worker-0 not found or logs unavailable. Skipping JAX core check.")
	}
}

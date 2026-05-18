//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/runtime"
	corescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var controllerCmd *exec.Cmd
var logFile *os.File
var repoRoot string
var k8sClient client.Client
var controllerBinPath = "/tmp/tpu_controller_e2e_bin"

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

	fmt.Println("=== Applying E2E RBAC ===")
	cmd = exec.Command("kubectl", "apply", "-f", "hack/e2e/tpu_device_plugin_rbac_e2e.yaml")
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to apply E2E RBAC: %v", err)
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	fmt.Println("=== Initializing k8sClient ===")
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatalf("Failed to build kubeconfig: %v", err)
	}
	scheme := runtime.NewScheme()
	if err := corescheme.AddToScheme(scheme); err != nil {
		log.Fatalf("Failed to add core scheme: %v", err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		log.Fatalf("Failed to add batch scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		log.Fatalf("Failed to add v1alpha1 to scheme: %v", err)
	}
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("Failed to create k8sClient: %v", err)
	}

	fmt.Println("=== Building Controller Binary ===")
	buildCmd := exec.Command("go", "build", "-o", controllerBinPath, "cmd/main.go")
	buildCmd.Dir = repoRoot
	if err := buildCmd.Run(); err != nil {
		log.Fatalf("Failed to build controller binary: %v", err)
	}

	fmt.Println("=== Starting Controller ===")
	logPath := "/tmp/controller_e2e.log"
	logFile, err = os.Create(logPath)
	if err != nil {
		log.Fatalf("Failed to create log file: %v", err)
	}

	controllerCmd = exec.Command(controllerBinPath, "--kube-config", kubeconfig)
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
		_ = controllerCmd.Wait()
	}
	if logFile != nil {
		logFile.Close()
	}
	_ = os.Remove(controllerBinPath)

	fmt.Println("=== Cleaning up E2E RBAC ===")
	cleanupCmd := exec.Command("kubectl", "delete", "-f", "hack/e2e/tpu_device_plugin_rbac_e2e.yaml", "--ignore-not-found")
	cleanupCmd.Dir = repoRoot
	_ = cleanupCmd.Run()
}

func cleanResources(t *testing.T, resourceTypes []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, rt := range resourceTypes {
		switch rt {
		case "tpunodegroups":
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &v1alpha1.TPUNodeGroupList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				for _, item := range list.Items {
					if !item.DeletionTimestamp.IsZero() {
						t.Logf("Waiting for TPUNodeGroup being deleted: %s", item.Name)
						return false, nil
					}
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for TPUNodeGroups deletion: %v", err)
			}
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.TPUNodeGroup{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete TPUNodeGroups: %v", err)
			}
		case "instancetemplates":
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &v1alpha1.InstanceTemplateList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				for _, item := range list.Items {
					if !item.DeletionTimestamp.IsZero() {
						t.Logf("Waiting for InstanceTemplate being deleted: %s", item.Name)
						return false, nil
					}
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for InstanceTemplates deletion: %v", err)
			}
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.InstanceTemplate{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete InstanceTemplates: %v", err)
			}
		case "workloadpolicies":
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &v1alpha1.WorkloadPolicyList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				for _, item := range list.Items {
					if !item.DeletionTimestamp.IsZero() {
						t.Logf("Waiting for WorkloadPolicy being deleted: %s", item.Name)
						return false, nil
					}
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for WorkloadPolicies deletion: %v", err)
			}
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.WorkloadPolicy{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete WorkloadPolicies: %v", err)
			}
		case "managedinstancegroups":
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &v1alpha1.ManagedInstanceGroupList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				for _, item := range list.Items {
					if !item.DeletionTimestamp.IsZero() {
						t.Logf("Waiting for ManagedInstanceGroup being deleted: %s", item.Name)
						return false, nil
					}
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for ManagedInstanceGroups deletion: %v", err)
			}
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.ManagedInstanceGroup{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete ManagedInstanceGroups: %v", err)
			}
		case "jobs":
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &batchv1.JobList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				for _, item := range list.Items {
					if !item.DeletionTimestamp.IsZero() {
						t.Logf("Waiting for Job being deleted: %s", item.Name)
						return false, nil
					}
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for Jobs deletion: %v", err)
			}
			background := metav1.DeletePropagationBackground
			if err := k8sClient.DeleteAllOf(ctx, &batchv1.Job{}, client.InNamespace("default"), client.PropagationPolicy(background)); err != nil {
				t.Fatalf("Failed to delete Jobs: %v", err)
			}
		case "nodes":
			var nodeList corev1.NodeList
			if err := k8sClient.List(ctx, &nodeList); err == nil {
				for _, node := range nodeList.Items {
					if _, ok := node.Labels["cloud.google.com/tpu-node-group"]; ok {
						t.Logf("Deleting stale Node object: %s", node.Name)
						if err := k8sClient.Delete(ctx, &node); err != nil && !errors.IsNotFound(err) {
							t.Fatalf("Failed to delete stale Node %s: %v", node.Name, err)
						}
					}
				}
			}
		default:
			t.Fatalf("Unknown resource type in cleanup: %s", rt)
		}
	}
}

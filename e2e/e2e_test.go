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

	"github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/runtime"
	corescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
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
	// Assuming we are in e2e, repo root is one level up.
	repoRoot = filepath.Dir(wd)

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

	fmt.Println("=== Cleaning up Stale Device Plugin DaemonSet ===")
	cmd = exec.Command("kubectl", "delete", "daemonset", "tpu-device-plugin", "-n", "kube-system", "--ignore-not-found")
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to delete stale device plugin DaemonSet: %v", err)
	}

	fmt.Println("=== Reseting Device Plugin RBAC State ===")
	cleanupCmd := exec.Command("kubectl", "delete", "-k", "deploy/deviceplugin", "--ignore-not-found")
	cleanupCmd.Dir = repoRoot
	_ = cleanupCmd.Run()

	fmt.Println("=== Applying Device Plugin Production RBAC & SA via Kustomize ===")
	applyCmd := exec.Command("kubectl", "apply", "-k", "deploy/deviceplugin")
	applyCmd.Dir = repoRoot
	if err := applyCmd.Run(); err != nil {
		log.Fatalf("Failed to apply device plugin components: %v", err)
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

	fmt.Println("=== Running E2E Target Cluster Safety Check ===")
	manifestPath := filepath.Join(repoRoot, "internal/controllers/tpunodegroup/testdata/test_nodegroup.yaml")
	yamlBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		log.Fatalf("Safety Check Error: Failed to read test manifest at %s: %v", manifestPath, err)
	}
	var ng v1alpha1.TPUNodeGroup
	if err := yaml.Unmarshal(yamlBytes, &ng); err != nil {
		log.Fatalf("Safety Check Error: Failed to unmarshal test manifest: %v", err)
	}

	expectedIP := ""
	if ng.Spec.BootstrapKubernetes != nil {
		expectedIP = ng.Spec.BootstrapKubernetes.ControlPlaneIP
	}
	if expectedIP == "" {
		log.Fatalf("Safety Check Error: ControlPlaneIP must be specified in %s", manifestPath)
	}

	var nodeList corev1.NodeList
	if err := k8sClient.List(context.Background(), &nodeList); err != nil {
		log.Fatalf("Safety Check Error: Failed to list nodes: %v", err)
	}

	var controlPlaneNode *corev1.Node
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			controlPlaneNode = node
			break
		}
		if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			controlPlaneNode = node
			break
		}
	}

	if controlPlaneNode == nil {
		log.Fatalf("Safety Check Error: No control-plane or master nodes found in K8s cluster. Ensure KUBECONFIG is configured correctly.")
	}

	actualIP := ""
	for _, addr := range controlPlaneNode.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			actualIP = addr.Address
			break
		}
	}

	if actualIP == "" {
		log.Fatalf("Safety Check Error: Failed to find InternalIP for control-plane node %s", controlPlaneNode.Name)
	}

	if actualIP != expectedIP {
		log.Fatalf("SAFETY ERROR: E2E test suite is running against a K8s cluster whose control-plane IP (%s) does NOT match the expected TPUNodeGroup controlPlaneIP (%s). Please ensure KUBECONFIG is set to the correct cluster (e.g. hack/e2e/remote-kubeconfig.yaml) and the SSH tunnel is active.", actualIP, expectedIP)
	}
	fmt.Printf("Safety check passed: Confirmed E2E is running against target cluster (Control Plane: %s, IP: %s)\n\n", controlPlaneNode.Name, actualIP)

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

	fmt.Println("=== Cleaning up Device Plugin Production RBAC & SA via Kustomize ===")
	cleanupCmd := exec.Command("kubectl", "delete", "-k", "deploy/deviceplugin", "--ignore-not-found")
	cleanupCmd.Dir = repoRoot
	_ = cleanupCmd.Run()
}

func cleanResources(t *testing.T, resourceTypes []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second) // Increased timeout to 120s as we actually wait for deletion now
	defer cancel()
	for _, rt := range resourceTypes {
		switch rt {
		case "tpunodegroups":
			t.Log("Deleting TPUNodeGroups...")
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.TPUNodeGroup{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete TPUNodeGroups: %v", err)
			}
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 300*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &v1alpha1.TPUNodeGroupList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				if len(list.Items) > 0 {
					t.Logf("Waiting for %d TPUNodeGroups to be deleted...", len(list.Items))
					return false, nil
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for TPUNodeGroups deletion: %v", err)
			}
		case "instancetemplates":
			t.Log("Deleting InstanceTemplates...")
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.InstanceTemplate{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete InstanceTemplates: %v", err)
			}
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &v1alpha1.InstanceTemplateList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				if len(list.Items) > 0 {
					t.Logf("Waiting for %d InstanceTemplates to be deleted...", len(list.Items))
					return false, nil
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for InstanceTemplates deletion: %v", err)
			}
		case "workloadpolicies":
			t.Log("Deleting WorkloadPolicies...")
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.WorkloadPolicy{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete WorkloadPolicies: %v", err)
			}
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &v1alpha1.WorkloadPolicyList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				if len(list.Items) > 0 {
					t.Logf("Waiting for %d WorkloadPolicies to be deleted...", len(list.Items))
					return false, nil
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for WorkloadPolicies deletion: %v", err)
			}
		case "managedinstancegroups":
			t.Log("Deleting ManagedInstanceGroups...")
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.ManagedInstanceGroup{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete ManagedInstanceGroups: %v", err)
			}
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &v1alpha1.ManagedInstanceGroupList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				if len(list.Items) > 0 {
					t.Logf("Waiting for %d ManagedInstanceGroups to be deleted...", len(list.Items))
					return false, nil
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for ManagedInstanceGroups deletion: %v", err)
			}
		case "jobs":
			t.Log("Deleting Jobs...")
			background := metav1.DeletePropagationBackground
			if err := k8sClient.DeleteAllOf(ctx, &batchv1.Job{}, client.InNamespace("default"), client.PropagationPolicy(background)); err != nil {
				t.Fatalf("Failed to delete Jobs: %v", err)
			}
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				list := &batchv1.JobList{}
				if err := k8sClient.List(ctx, list, client.InNamespace("default")); err != nil {
					return false, err
				}
				if len(list.Items) > 0 {
					t.Logf("Waiting for %d Jobs to be deleted...", len(list.Items))
					return false, nil
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for Jobs deletion: %v", err)
			}
		case "nodes":
			t.Log("Deleting stale Node objects...")
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
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
				var nodeList corev1.NodeList
				if err := k8sClient.List(ctx, &nodeList); err != nil {
					return false, err
				}
				found := false
				for _, node := range nodeList.Items {
					if _, ok := node.Labels["cloud.google.com/tpu-node-group"]; ok {
						found = true
						break
					}
				}
				if found {
					t.Log("Waiting for stale Node objects to be deleted...")
					return false, nil
				}
				return true, nil
			})
			if err != nil {
				t.Fatalf("Failed waiting for Node deletion: %v", err)
			}
		default:
			t.Fatalf("Unknown resource type in cleanup: %s", rt)
		}
	}
}

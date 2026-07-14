//go:build e2e

package e2e

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

var (
	controllerCmd     *exec.Cmd
	logFile           *os.File
	repoRoot          string
	k8sClient         client.Client
	kubernetesClient  *kubernetes.Clientset
	controllerBinPath = "/tmp/tpu_controller_e2e_bin"
	skipTeardown      = flag.Bool("skip-teardown", false, "Skip teardown on failure for manual inspection")
)

func TestMain(m *testing.M) {
	flag.Parse()
	setup()
	code := m.Run()
	if code != 0 {
		fmt.Println("=== E2E Tests Failed - Dumping Controller Logs ===")
		dumpControllerLogs()
		if *skipTeardown {
			fmt.Println("=== SKIPPING TEARDOWN for manual inspection ===")
			os.Exit(code)
		}
	}
	teardown()
	os.Exit(code)
}

func dumpControllerLogs() {
	if k8sClient == nil || kubernetesClient == nil {
		fmt.Println("Clients are nil, cannot dump logs")
		return
	}
	ctx := context.Background()
	var podList corev1.PodList
	selector := labels.SelectorFromSet(labels.Set{"control-plane": "tpu-node-group-controller"})
	err := k8sClient.List(ctx, &podList, client.InNamespace("tpu-node-group"), client.MatchingLabelsSelector{Selector: selector})
	if err != nil {
		fmt.Printf("Failed to list controller pods for log dumping: %v\n", err)
		return
	}

	for _, pod := range podList.Items {
		fmt.Printf("\n--- Logs for Controller Pod: %s ---\n", pod.Name)
		req := kubernetesClient.CoreV1().Pods("tpu-node-group").GetLogs(pod.Name, &corev1.PodLogOptions{})
		logs, err := req.Stream(ctx)
		if err != nil {
			fmt.Printf("Failed to get logs for pod %s: %v\n", pod.Name, err)
			continue
		}
		defer logs.Close()
		_, err = io.Copy(os.Stdout, logs)
		if err != nil {
			fmt.Printf("Failed to copy logs for pod %s: %v\n", pod.Name, err)
		}
		fmt.Printf("--- End of logs for %s ---\n", pod.Name)
	}
}

func setup() {
	fmt.Println("=== Global Setup ===")
	Config.BindEnv()

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
	kubernetesClient, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Failed to create kubernetesClient: %v", err)
	}

	fmt.Println("=== Running E2E Target Cluster Safety Check ===")
	manifestPath := filepath.Join(repoRoot, "internal/controllers/tpunodegroup/testdata/test_nodegroup.yaml")
	yamlBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		log.Fatalf("Safety Check Error: Failed to read test manifest at %s: %v", manifestPath, err)
	}
	var ng v1alpha1.TPUNodeGroup
	expandedYAML := expandManifest(yamlBytes)
	if err := yaml.Unmarshal(expandedYAML, &ng); err != nil {
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
		log.Fatalf("SAFETY ERROR: E2E test suite is running against a K8s cluster whose control-plane IP (%s) does NOT match the expected TPUNodeGroup controlPlaneIP (%s). Please ensure KUBECONFIG is set to the correct cluster (e.g. e2e/remote-kubeconfig.yaml) and the SSH tunnel is active.", actualIP, expectedIP)
	}
	fmt.Printf("Safety check passed: Confirmed E2E is running against target cluster (Control Plane: %s, IP: %s)\n\n", controlPlaneNode.Name, actualIP)

	fmt.Println("=== Preparing E2E Kustomize Deployment ===")
	kustomizeCmd := exec.Command("make", "e2e-kustomize")
	kustomizeCmd.Dir = repoRoot
	if err := kustomizeCmd.Run(); err != nil {
		log.Fatalf("Failed to generate e2e kustomization: %v", err)
	}

	fmt.Println("=== Deploying Controller to Cluster ===")
	deployCmd := exec.Command("kubectl", "apply", "-k", "e2e/deploy")
	deployCmd.Dir = repoRoot
	if err := deployCmd.Run(); err != nil {
		log.Fatalf("Failed to deploy controller via kustomize: %v", err)
	}

	fmt.Println("=== Copying Image Pull Secret ===")
	sourceSecret := &corev1.Secret{}
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "gcr-json-key", Namespace: "kube-system"}, sourceSecret)
	if err != nil {
		log.Fatalf("CRITICAL ERROR: Failed to find image pull secret 'gcr-json-key' in kube-system namespace. This is required for E2E. Error: %v", err)
	}

	targetSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gcr-json-key",
			Namespace: "tpu-node-group",
		},
		Data: sourceSecret.Data,
		Type: sourceSecret.Type,
	}
	err = k8sClient.Patch(context.Background(), targetSecret, client.Apply, client.ForceOwnership, client.FieldOwner("e2e-test"))
	if err != nil {
		log.Fatalf("Failed to copy image pull secret to tpu-node-group namespace: %v", err)
	}

	fmt.Println("=== Waiting for Controller to be Ready ===")
	// Poll for controller deployment readiness
	err = wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		deployment := &appsv1.Deployment{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "tpu-node-group-controller-manager", Namespace: "tpu-node-group"}, deployment)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if deployment.Status.ReadyReplicas > 0 {
			fmt.Println("Controller deployment is ready!")
			return true, nil
		}
		fmt.Printf("Waiting for controller deployment readiness... (Ready=%d)\n", deployment.Status.ReadyReplicas)
		return false, nil
	})
	if err != nil {
		log.Fatalf("Controller deployment failed to become ready within 2 minutes: %v", err)
	}
}

func teardown() {
	fmt.Println("=== Global Teardown ===")
	fmt.Println("=== Cleaning up Controller Deployment via Kustomize ===")
	cleanupDeployCmd := exec.Command("kubectl", "delete", "-k", "e2e/deploy", "--ignore-not-found")
	cleanupDeployCmd.Dir = repoRoot
	_ = cleanupDeployCmd.Run()

	fmt.Println("=== Cleaning up Device Plugin Production RBAC & SA via Kustomize ===")
	cleanupCmd := exec.Command("kubectl", "delete", "-k", "deploy/deviceplugin", "--ignore-not-found")
	cleanupCmd.Dir = repoRoot
	_ = cleanupCmd.Run()

	if logFile != nil {
		logFile.Close()
	}
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
			if err := waitForAllDeleted(ctx, k8sClient, &v1alpha1.TPUNodeGroupList{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed waiting for TPUNodeGroups deletion: %v", err)
			}
		case "instancetemplates":
			t.Log("Deleting InstanceTemplates...")
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.InstanceTemplate{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete InstanceTemplates: %v", err)
			}
			if err := waitForAllDeleted(ctx, k8sClient, &v1alpha1.InstanceTemplateList{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed waiting for InstanceTemplates deletion: %v", err)
			}
		case "workloadpolicies":
			t.Log("Deleting WorkloadPolicies...")
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.WorkloadPolicy{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete WorkloadPolicies: %v", err)
			}
			if err := waitForAllDeleted(ctx, k8sClient, &v1alpha1.WorkloadPolicyList{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed waiting for WorkloadPolicies deletion: %v", err)
			}
		case "managedinstancegroups":
			t.Log("Deleting ManagedInstanceGroups...")
			if err := k8sClient.DeleteAllOf(ctx, &v1alpha1.ManagedInstanceGroup{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed to delete ManagedInstanceGroups: %v", err)
			}
			if err := waitForAllDeleted(ctx, k8sClient, &v1alpha1.ManagedInstanceGroupList{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed waiting for ManagedInstanceGroups deletion: %v", err)
			}
		case "jobs":
			t.Log("Deleting Jobs...")
			background := metav1.DeletePropagationBackground
			if err := k8sClient.DeleteAllOf(ctx, &batchv1.Job{}, client.InNamespace("default"), client.PropagationPolicy(background)); err != nil {
				t.Fatalf("Failed to delete Jobs: %v", err)
			}
			if err := waitForAllDeleted(ctx, k8sClient, &batchv1.JobList{}, client.InNamespace("default")); err != nil {
				t.Fatalf("Failed waiting for Jobs deletion: %v", err)
			}
		case "nodes":
			t.Log("Deleting stale Node objects...")
			var nodeList corev1.NodeList
			if err := k8sClient.List(ctx, &nodeList); err == nil {
				for _, node := range nodeList.Items {
					_, hasName := node.Labels["cloud.google.com/tpu-node-group-name"]
					_, hasNamespace := node.Labels["cloud.google.com/tpu-node-group-namespace"]
					if hasName && hasNamespace {
						t.Logf("Deleting stale Node object: %s", node.Name)
						if err := k8sClient.Delete(ctx, &node); err != nil && !errors.IsNotFound(err) {
							t.Fatalf("Failed to delete stale Node %s: %v", node.Name, err)
						}
					}
				}
			}
			selector, _ := labels.Parse("cloud.google.com/tpu-node-group-namespace,cloud.google.com/tpu-node-group-name")
			if err := waitForAllDeleted(ctx, k8sClient, &corev1.NodeList{}, client.MatchingLabelsSelector{Selector: selector}); err != nil {
				t.Fatalf("Failed waiting for Node deletion: %v", err)
			}
		default:
			t.Fatalf("Unknown resource type in cleanup: %s", rt)
		}
	}
}

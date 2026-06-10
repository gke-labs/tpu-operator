package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHelpers_WaitForCondition(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)

	key := types.NamespacedName{Name: "test-obj", Namespace: "default"}
	initialObj := &v1alpha1.InstanceTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Status: v1alpha1.InstanceTemplateStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(initialObj).WithStatusSubresource(initialObj).Build()

	ctx := context.Background()
	err := waitForCondition(ctx, fakeClient, key, &v1alpha1.InstanceTemplate{}, func(obj *v1alpha1.InstanceTemplate) []metav1.Condition {
		return obj.Status.Conditions
	}, "Ready", metav1.ConditionTrue, 5*time.Second)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestHelpers_WaitForCondition_NotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)

	key := types.NamespacedName{Name: "non-existent-obj", Namespace: "default"}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	ctx := context.Background()
	err := waitForCondition(ctx, fakeClient, key, &v1alpha1.InstanceTemplate{}, func(obj *v1alpha1.InstanceTemplate) []metav1.Condition {
		return obj.Status.Conditions
	}, "Ready", metav1.ConditionTrue, 5*time.Second)

	if err == nil {
		t.Fatal("Expected error when resource is not found, got nil")
	}
	expectedErrMsg := "resource non-existent-obj was deleted while waiting for condition Ready"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message %q, got %q", expectedErrMsg, err.Error())
	}
}

func TestHelpers_WaitForDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)

	key := types.NamespacedName{Name: "test-obj", Namespace: "default"}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	ctx := context.Background()
	err := waitForDeletion(ctx, fakeClient, key, &v1alpha1.InstanceTemplate{}, 5*time.Second)
	if err != nil {
		t.Fatalf("Expected no error for not found object, got %v", err)
	}
}

func TestHelpers_ApplyManifest(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)

	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "test.yaml")
	content := []byte(`
apiVersion: tpu.google.com/v1alpha1
kind: InstanceTemplate
metadata:
  name: test-apply
  namespace: default
spec:
  project: test-project
  machineType: v4-8
`)
	if err := os.WriteFile(yamlPath, content, 0644); err != nil {
		t.Fatalf("Failed to write temp yaml: %v", err)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	obj := &v1alpha1.InstanceTemplate{}
	err := applyManifest(ctx, fakeClient, yamlPath, obj)
	if err != nil {
		t.Fatalf("applyManifest failed: %v", err)
	}

	var fetched v1alpha1.InstanceTemplate
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-apply", Namespace: "default"}, &fetched); err != nil {
		t.Fatalf("Failed to get applied object: %v", err)
	}
	if fetched.Spec.Project != "test-project" {
		t.Errorf("Expected project test-project, got %s", fetched.Spec.Project)
	}
}

func TestHelpers_ApplyManifest_WithConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)

	// Save original config and restore on cleanup
	origConfig := Config
	t.Cleanup(func() {
		Config = origConfig
	})

	Config.Project = "my-config-project"

	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "test.yaml")
	content := []byte(`
apiVersion: tpu.google.com/v1alpha1
kind: InstanceTemplate
metadata:
  name: test-apply-config
  namespace: default
spec:
  project: ${E2E_PROJECT}
  machineType: v4-8
`)
	if err := os.WriteFile(yamlPath, content, 0644); err != nil {
		t.Fatalf("Failed to write temp yaml: %v", err)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	obj := &v1alpha1.InstanceTemplate{}
	err := applyManifest(ctx, fakeClient, yamlPath, obj)
	if err != nil {
		t.Fatalf("applyManifest failed: %v", err)
	}

	var fetched v1alpha1.InstanceTemplate
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-apply-config", Namespace: "default"}, &fetched); err != nil {
		t.Fatalf("Failed to get applied object: %v", err)
	}
	if fetched.Spec.Project != "my-config-project" {
		t.Errorf("Expected project my-config-project, got %s", fetched.Spec.Project)
	}
}

func TestHelpers_ApplyManifest_WithBindEnv(t *testing.T) {
	// Save original config and restore on cleanup
	origConfig := Config
	t.Cleanup(func() {
		Config = origConfig
	})

	// Set env variable
	t.Setenv("E2E_PROJECT", "env-bind-project")

	// Clear config.Project to force bind from env
	Config.Project = ""
	Config.BindEnv()

	if Config.Project != "env-bind-project" {
		t.Errorf("Expected bound project env-bind-project, got %s", Config.Project)
	}
}

func TestHelpers_RegionPrecedence(t *testing.T) {
	origConfig := Config
	t.Cleanup(func() {
		Config = origConfig
	})

	// Case 1: Flag overrides Env
	Config.Region = "flag-region"
	t.Setenv("E2E_REGION", "env-region")
	Config.BindEnv()
	if Config.Region != "flag-region" {
		t.Errorf("Expected Region to remain flag-region, got %s", Config.Region)
	}

	// Case 2: Flag empty, falls back to Env
	Config.Region = ""
	t.Setenv("E2E_REGION", "env-region")
	Config.BindEnv()
	if Config.Region != "env-region" {
		t.Errorf("Expected Region to fall back to env-region, got %s", Config.Region)
	}

	// Case 3: Both empty, falls back to Default
	Config.Region = ""
	t.Setenv("E2E_REGION", "")
	Config.BindEnv()
	if Config.Region != "us-central1" {
		t.Errorf("Expected Region to fall back to us-central1 default, got %s", Config.Region)
	}
}




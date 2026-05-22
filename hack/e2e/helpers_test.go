package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gke-labs/tpu-operator/pkg/apis/tpu/v1alpha1"
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

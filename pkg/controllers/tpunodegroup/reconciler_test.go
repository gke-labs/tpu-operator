package tpunodegroup

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/generated/clientset/versioned/fake"
	listers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/listers/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
)

func TestReconcile(t *testing.T) {
	// 1. Create a fake clientset
	fakeClient := fake.NewClientset()

	// 2. Create a store and lister for TPUNodeGroup
	store := cache.NewIndexer(cache.DeletionHandlingMetaNamespaceKeyFunc, cache.Indexers{})
	lister := listers.NewTPUNodeGroupLister(store)

	// 3. Create a test TPUNodeGroup
	tpuNodeGroup := &tpuapi.TPUNodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tpu",
			Namespace: "default",
		},
		Spec: tpuapi.TPUNodeGroupSpec{
			Project:      "test-project",
			NodeLocation: "us-central1-a",
			NodeCount:    1,
		},
	}

	// Add it to the store
	store.Add(tpuNodeGroup)

	// 4. Instantiate the reconciler
	k8sFakeClient := k8sfake.NewSimpleClientset()
	r := NewReconciler(k8sFakeClient, fakeClient, lister, &gce.MockIGMClient{}, &gce.MockInstanceClient{})

	// 5. Call Reconcile
	objectRef := cache.ObjectName{Namespace: "default", Name: "test-tpu"}
	err := r.Reconcile(context.Background(), objectRef)

	// 6. Assertions
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// TODO: Add more assertions as sub-reconcilers are implemented.
}

func TestReconcile_NotFound(t *testing.T) {
	fakeClient := fake.NewClientset()
	store := cache.NewIndexer(cache.DeletionHandlingMetaNamespaceKeyFunc, cache.Indexers{})
	lister := listers.NewTPUNodeGroupLister(store)

	k8sFakeClient := k8sfake.NewSimpleClientset()
	r := NewReconciler(k8sFakeClient, fakeClient, lister, &gce.MockIGMClient{}, &gce.MockInstanceClient{})

	objectRef := cache.ObjectName{Namespace: "default", Name: "non-existent"}
	err := r.Reconcile(context.Background(), objectRef)

	if err != nil {
		t.Fatalf("Expected no error for NotFound case, got %v", err)
	}
}

func TestReconcile_DevicePlugin(t *testing.T) {
	fakeClient := fake.NewClientset()
	store := cache.NewIndexer(cache.DeletionHandlingMetaNamespaceKeyFunc, cache.Indexers{})
	lister := listers.NewTPUNodeGroupLister(store)

	tpuNodeGroup := &tpuapi.TPUNodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tpu",
			Namespace: "default",
		},
		Spec: tpuapi.TPUNodeGroupSpec{
			Project:      "test-project",
			NodeLocation: "us-central1-a",
			NodeCount:    1,
		},
	}
	store.Add(tpuNodeGroup)

	k8sFakeClient := k8sfake.NewSimpleClientset()
	r := NewReconciler(k8sFakeClient, fakeClient, lister, &gce.MockIGMClient{}, &gce.MockInstanceClient{})

	objectRef := cache.ObjectName{Namespace: "default", Name: "test-tpu"}
	err := r.Reconcile(context.Background(), objectRef)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify DaemonSet creation
	ds, err := k8sFakeClient.AppsV1().DaemonSets("default").Get(context.Background(), "tpu-device-plugin", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected DaemonSet to be created, got error: %v", err)
	}
	if ds.Name != "tpu-device-plugin" {
		t.Errorf("Expected DaemonSet name %s, got %s", "tpu-device-plugin", ds.Name)
	}
}




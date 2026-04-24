package tpunodegroup

import (
	"context"
	"testing"
	"time"

	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	fakeclientset "gke-internal.googlesource.com/tpu-node-group/pkg/generated/clientset/versioned/fake"
	informers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	fakekubernetes "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestTPUNodeGroupController(t *testing.T) {
	err := tpuapi.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("Error adding to scheme: %v", err)
	}

	ctx := context.Background()
	kubeClient := fakekubernetes.NewClientset()

	// Use NewClientset instead of NewSimpleClientset as recommended
	tpuClient := fakeclientset.NewClientset()

	tpuInformerFactory := informers.NewSharedInformerFactory(tpuClient, time.Second*30)
	informer := tpuInformerFactory.Tpu().V1alpha1().TPUNodeGroups()

	controller := NewController(ctx, kubeClient, tpuClient, informer)

	stopCh := make(chan struct{})
	defer close(stopCh)
	tpuInformerFactory.Start(stopCh)

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

	err = tpuClient.Tracker().Add(tpuNodeGroup)
	if err != nil {
		t.Fatalf("Error adding TPUNodeGroup to tracker: %v", err)
	}

	err = informer.Informer().GetIndexer().Add(tpuNodeGroup)
	if err != nil {
		t.Fatalf("Error adding to informer cache: %v", err)
	}

	objectRef := cache.ObjectName{Namespace: "default", Name: "test-tpu"}
	err = controller.syncHandler(ctx, objectRef)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

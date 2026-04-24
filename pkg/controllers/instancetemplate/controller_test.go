package instancetemplate

import (
	"context"
	"testing"
	"time"

	api "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	fakeclientset "gke-internal.googlesource.com/tpu-node-group/pkg/generated/clientset/versioned/fake"
	informers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekubernetes "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestInstanceTemplateController(t *testing.T) {
	err := api.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("Error adding to scheme: %v", err)
	}

	kubeClient := fakekubernetes.NewSimpleClientset()
	tpuClient := fakeclientset.NewSimpleClientset()

	tpuInformerFactory := informers.NewSharedInformerFactory(tpuClient, time.Second*30)
	informer := tpuInformerFactory.Tpu().V1alpha1().InstanceTemplates()

	controller := NewController(kubeClient, tpuClient, informer)

	stopCh := make(chan struct{})
	defer close(stopCh)
	tpuInformerFactory.Start(stopCh)

	it := &api.InstanceTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: api.InstanceTemplateSpec{
			InstanceConfig: api.InstanceConfig{
				MachineType: "tpu7x-standard-4t",
			},
		},
	}
	_, err = tpuClient.TpuV1alpha1().InstanceTemplates("default").Create(context.TODO(), it, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Error creating instance template: %v", err)
	}

	err = informer.Informer().GetIndexer().Add(it)
	if err != nil {
		t.Fatalf("Error adding to informer cache: %v", err)
	}

	err = controller.syncHandler("default/test-template")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

package client

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"gke-internal.googlesource.com/tpu-node-group/pkg/api"
)

// Interface is the minimal clientset interface needed for informers.
type Interface interface {
	TPUV1alpha1() TPUV1alpha1Interface
}

// TPUV1alpha1Interface provides access to TPUNodeGroups.
type TPUV1alpha1Interface interface {
	TPUNodeGroups(namespace string) TPUNodeGroupInterface
}

// TPUNodeGroupInterface provides methods to work with TPUNodeGroup resources.
type TPUNodeGroupInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*api.TPUNodeGroupList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
}

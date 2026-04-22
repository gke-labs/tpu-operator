package informers

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"gke-internal.googlesource.com/tpu-node-group/pkg/api"
	"gke-internal.googlesource.com/tpu-node-group/pkg/client"
	"gke-internal.googlesource.com/tpu-node-group/pkg/listers"
)

// TPUNodeGroupInformer provides access to a shared informer and lister for TPUNodeGroups.
type TPUNodeGroupInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() listers.TPUNodeGroupLister
}

type tpuNodeGroupInformer struct {
	client       client.Interface
	namespace    string
	resyncPeriod time.Duration
	indexers     cache.Indexers
	informer     cache.SharedIndexInformer
}

// NewTPUNodeGroupInformer constructs a new informer for TPUNodeGroup type.
func NewTPUNodeGroupInformer(client client.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) TPUNodeGroupInformer {
	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			return client.TPUV1alpha1().TPUNodeGroups(namespace).List(context.Background(), opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			return client.TPUV1alpha1().TPUNodeGroups(namespace).Watch(context.Background(), opts)
		},
	}

	informer := cache.NewSharedIndexInformer(lw, &api.TPUNodeGroup{}, resyncPeriod, indexers)

	return &tpuNodeGroupInformer{
		client:       client,
		namespace:    namespace,
		resyncPeriod: resyncPeriod,
		indexers:     indexers,
		informer:     informer,
	}
}

func (i *tpuNodeGroupInformer) Informer() cache.SharedIndexInformer {
	return i.informer
}

func (i *tpuNodeGroupInformer) Lister() listers.TPUNodeGroupLister {
	return listers.NewTPUNodeGroupLister(i.informer.GetIndexer())
}

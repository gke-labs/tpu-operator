package listers

import (
	labels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	listers "k8s.io/client-go/listers"
	cache "k8s.io/client-go/tools/cache"
	api "gke-internal.googlesource.com/tpu-node-group/pkg/api"
)

// TPUNodeGroupLister helps list TPUNodeGroups.
// All objects returned here must be treated as read-only.
type TPUNodeGroupLister interface {
	// List lists all TPUNodeGroups in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*api.TPUNodeGroup, err error)
	// TPUNodeGroups returns an object that can list and get TPUNodeGroups.
	TPUNodeGroups(namespace string) TPUNodeGroupNamespaceLister
	TPUNodeGroupListerExpansion
}

// tpuNodeGroupLister implements the TPUNodeGroupLister interface.
type tpuNodeGroupLister struct {
	listers.ResourceIndexer[*api.TPUNodeGroup]
}

// NewTPUNodeGroupLister returns a new TPUNodeGroupLister.
func NewTPUNodeGroupLister(indexer cache.Indexer) TPUNodeGroupLister {
	return &tpuNodeGroupLister{listers.New[*api.TPUNodeGroup](indexer, schema.GroupResource{Group: "tpu.google.com", Resource: "tpunodegroups"})}
}

// TPUNodeGroups returns an object that can list and get TPUNodeGroups.
func (s *tpuNodeGroupLister) TPUNodeGroups(namespace string) TPUNodeGroupNamespaceLister {
	return tpuNodeGroupNamespaceLister{listers.NewNamespaced[*api.TPUNodeGroup](s.ResourceIndexer, namespace)}
}

// TPUNodeGroupNamespaceLister helps list and get TPUNodeGroups.
// All objects returned here must be treated as read-only.
type TPUNodeGroupNamespaceLister interface {
	// List lists all TPUNodeGroups in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*api.TPUNodeGroup, err error)
	// Get retrieves the TPUNodeGroup from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*api.TPUNodeGroup, error)
	TPUNodeGroupNamespaceListerExpansion
}

// tpuNodeGroupNamespaceLister implements the TPUNodeGroupNamespaceLister interface.
type tpuNodeGroupNamespaceLister struct {
	listers.ResourceIndexer[*api.TPUNodeGroup]
}

// TPUNodeGroupListerExpansion allows custom methods to be added to TPUNodeGroupLister.
type TPUNodeGroupListerExpansion interface{}

// TPUNodeGroupNamespaceListerExpansion allows custom methods to be added to TPUNodeGroupNamespaceLister.
type TPUNodeGroupNamespaceListerExpansion interface{}

package tpunodegroup

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	clientset "gke-internal.googlesource.com/tpu-node-group/pkg/generated/clientset/versioned"
	informers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/informers/externalversions/tpu/v1alpha1"
	listers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/listers/tpu/v1alpha1"
)

const controllerAgentName = "tpunodegroup-controller"

// Controller is the controller implementation for TPUNodeGroup resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// tpuclientset is a clientset for the TPUNodeGroup API
	tpuclientset clientset.Interface

	tpuNodeGroupsLister listers.TPUNodeGroupLister
	tpuNodeGroupsSynced cache.InformerSynced

	// workqueue is a rate limited work queue.
	workqueue workqueue.TypedRateLimitingInterface[cache.ObjectName]

	// reconciler handles the business logic of reconciliation
	reconciler *TPUNodeGroupReconciler
}

// NewController returns a new tpunodegroup controller
func NewController(
	ctx context.Context,
	kubeclientset kubernetes.Interface,
	tpuclientset clientset.Interface,
	tpuNodeGroupInformer informers.TPUNodeGroupInformer) *Controller {
	logger := klog.FromContext(ctx)

	ratelimiter := workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[cache.ObjectName](5*time.Millisecond, 1000*time.Second),
		&workqueue.TypedBucketRateLimiter[cache.ObjectName]{Limiter: rate.NewLimiter(rate.Limit(50), 300)},
	)

	controller := &Controller{
		kubeclientset:       kubeclientset,
		tpuclientset:        tpuclientset,
		tpuNodeGroupsLister: tpuNodeGroupInformer.Lister(),
		tpuNodeGroupsSynced: tpuNodeGroupInformer.Informer().HasSynced,
		workqueue:           workqueue.NewTypedRateLimitingQueue(ratelimiter),
		reconciler:          NewReconciler(tpuclientset, tpuNodeGroupInformer.Lister()),
	}

	logger.Info("Setting up event handlers")
	// Set up an event handler for when TPUNodeGroup resources change
	tpuNodeGroupInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueTPUNodeGroup,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueTPUNodeGroup(new)
		},
		DeleteFunc: controller.enqueueTPUNodeGroup,
	})

	return controller
}

// Run will set up the event handlers, as well as syncing informer caches and starting workers.
func (c *Controller) Run(ctx context.Context, workers int) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()
	logger := klog.FromContext(ctx)

	logger.Info("Starting TPUNodeGroup controller")

	logger.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(ctx.Done(), c.tpuNodeGroupsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	logger.Info("Starting workers", "count", workers)
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	logger.Info("Started workers")
	<-ctx.Done()
	logger.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem(ctx context.Context) bool {
	objRef, shutdown := c.workqueue.Get()
	logger := klog.FromContext(ctx)

	if shutdown {
		return false
	}

	defer c.workqueue.Done(objRef)

	err := c.syncHandler(ctx, objRef)
	if err == nil {
		c.workqueue.Forget(objRef)
		logger.Info("Successfully synced", "objectName", objRef)
		return true
	}

	utilruntime.HandleErrorWithContext(ctx, err, "Error syncing; requeuing for later retry", "objectReference", objRef)
	c.workqueue.AddRateLimited(objRef)
	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two.
func (c *Controller) syncHandler(ctx context.Context, objectRef cache.ObjectName) error {
	// Delegate the work to the reconciler
	return c.reconciler.Reconcile(ctx, objectRef)
}

// enqueueTPUNodeGroup takes a TPUNodeGroup resource and converts it into a namespace/name
// string which is then put onto the work queue.
func (c *Controller) enqueueTPUNodeGroup(obj interface{}) {
	if objectRef, err := cache.ObjectToName(obj); err != nil {
		utilruntime.HandleError(err)
		return
	} else {
		c.workqueue.Add(objectRef)
	}
}

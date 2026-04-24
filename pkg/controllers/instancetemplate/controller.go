package instancetemplate

import (
	"fmt"
	"time"

	clientset "gke-internal.googlesource.com/tpu-node-group/pkg/generated/clientset/versioned"
	informers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/informers/externalversions/tpu/v1alpha1"
	listers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/listers/tpu/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const controllerAgentName = "instancetemplate-controller"

// Controller is the controller implementation for InstanceTemplate resources
type Controller struct {
	kubeclientset kubernetes.Interface
	tpuclientset  clientset.Interface

	instanceTemplatesLister listers.InstanceTemplateLister
	instanceTemplatesSynced cache.InformerSynced

	workqueue workqueue.TypedRateLimitingInterface[string]
	recorder  record.EventRecorder
}

// NewController returns a new instancetemplate controller
func NewController(kubeclientset kubernetes.Interface, tpuclientset clientset.Interface, instanceTemplateInformer informers.InstanceTemplateInformer) *Controller {

	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:           kubeclientset,
		tpuclientset:           tpuclientset,
		instanceTemplatesLister: instanceTemplateInformer.Lister(),
		instanceTemplatesSynced: instanceTemplateInformer.Informer().HasSynced,
		workqueue:               workqueue.NewTypedRateLimitingQueueWithConfig[string](workqueue.DefaultTypedControllerRateLimiter[string](), workqueue.TypedRateLimitingQueueConfig[string]{Name: "InstanceTemplates"}),
		recorder:               recorder,
	}

	klog.Info("Setting up event handlers")
	instanceTemplateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueInstanceTemplate,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueInstanceTemplate(new)
		},
		DeleteFunc: controller.enqueueInstanceTemplate,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("Starting InstanceTemplate controller")

	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.instanceTemplatesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	key, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	err := func(key string) error {
		defer c.workqueue.Done(key)
		if err := c.syncHandler(key); err != nil {
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing %q: %s, requeuing", key, err.Error())
		}
		c.workqueue.Forget(key)
		klog.Infof("Successfully synced %q", key)
		return nil
	}(key)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	instanceTemplate, err := c.instanceTemplatesLister.InstanceTemplates(namespace).Get(name)
	if errors.IsNotFound(err) {
		utilruntime.HandleError(fmt.Errorf("instancetemplate %q in workqueue no longer exists", key))
		return nil
	}

	if err != nil {
		return err
	}

	klog.Infof("Reconciling InstanceTemplate %s/%s", namespace, name)
	klog.Infof("Spec: %+v", instanceTemplate.Spec)

	c.recorder.Event(instanceTemplate, corev1.EventTypeNormal, "Synced", "InstanceTemplate synced successfully")
	return nil
}

func (c *Controller) enqueueInstanceTemplate(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

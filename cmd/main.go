package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	tpuv1alpha1 "github.com/gke-labs/tpu-operator/pkg/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/pkg/controllers/instancetemplate"
	"github.com/gke-labs/tpu-operator/pkg/controllers/managedinstancegroup"
	"github.com/gke-labs/tpu-operator/pkg/controllers/tpunodegroup"
	"github.com/gke-labs/tpu-operator/pkg/controllers/workloadpolicy"
	"github.com/gke-labs/tpu-operator/pkg/gce"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	masterURL  string
	kubeconfig string
	setupLog   = ctrl.Log.WithName("setup")
)

func main() {
	klog.InitFlags(nil)
	// Renamed to "kube-config" to avoid collision with "kubeconfig" flag defined by dependencies.
	flag.StringVar(&kubeconfig, "kube-config", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig.")
	flag.Parse()

	ctrl.SetLogger(klogr.New())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		setupLog.Error(err, "Error building kubeconfig")
		os.Exit(1)
	}

	// Create standard Kubernetes clientset
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		setupLog.Error(err, "Error creating kubernetes client")
		os.Exit(1)
	}

	// Create GCE Manager
	gceManager, err := gce.NewManager(ctx)
	if err != nil {
		setupLog.Error(err, "Error creating GCE manager")
		os.Exit(1)
	}
	defer gceManager.Close()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		setupLog.Error(err, "Error adding corev1 scheme")
		os.Exit(1)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		setupLog.Error(err, "Error adding appsv1 scheme")
		os.Exit(1)
	}
	if err := tpuv1alpha1.AddToScheme(scheme); err != nil {
		setupLog.Error(err, "Error adding tpuv1alpha1 scheme")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&tpuv1alpha1.TPUNodeGroup{}:        {},
				&tpuv1alpha1.InstanceTemplate{}:    {},
				&tpuv1alpha1.ManagedInstanceGroup{}: {},
				&tpuv1alpha1.WorkloadPolicy{}:       {},
				&corev1.Node{}:                      {},
				&appsv1.DaemonSet{}:                 {},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "Error creating manager")
		os.Exit(1)
	}

	if err = (&instancetemplate.InstanceTemplateReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controller").WithName("instancetemplate"),
		GCE:    gceManager.InstanceTemplates(),
		GCEOps: gceManager.GlobalOperations(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Error setting up controller")
		os.Exit(1)
	}

	if err = tpunodegroup.NewTPUNodeGroupReconciler(mgr.GetClient(), mgr.GetScheme(), kubeClient, gceManager.IGM(), gceManager.Instances(), ctrl.Log.WithName("controller").WithName("tpunodegroup")).
		SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Error setting up TPU Node Group controller")
		os.Exit(1)
	}
	if err = (&workloadpolicy.WorkloadPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controller").WithName("workloadpolicy"),
		GCE:    gceManager.ResourcePolicies(),
		GCEOps: gceManager.RegionOperations(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Error setting up WorkloadPolicy controller")
		os.Exit(1)
	}

	if err = (&managedinstancegroup.ManagedInstanceGroupReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controller").WithName("managedinstancegroup"),
		GCE:    gceManager.IGM(),
		GCEOps: gceManager.ZoneOperations(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Error setting up ManagedInstanceGroup controller")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "Error running manager")
		os.Exit(1)
	}
}

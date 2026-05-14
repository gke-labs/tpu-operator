package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/controllers/instancetemplate"
	"gke-internal.googlesource.com/tpu-node-group/pkg/controllers/managedinstancegroup"
	"gke-internal.googlesource.com/tpu-node-group/pkg/controllers/tpunodegroup"
	"gke-internal.googlesource.com/tpu-node-group/pkg/controllers/workloadpolicy"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
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

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{})
	if err != nil {
		setupLog.Error(err, "Error creating manager")
		os.Exit(1)
	}

	if err := tpuv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		setupLog.Error(err, "Error adding scheme")
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

package main

import (
       "context"
       "flag"
       "os/signal"
       "syscall"
       "time"

       tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
       "gke-internal.googlesource.com/tpu-node-group/pkg/controllers/instancetemplate"
       "gke-internal.googlesource.com/tpu-node-group/pkg/controllers/tpunodegroup"
       "gke-internal.googlesource.com/tpu-node-group/pkg/gce"
       tpuclientset "gke-internal.googlesource.com/tpu-node-group/pkg/generated/clientset/versioned"
       tpuinformers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/informers/externalversions"
       "k8s.io/client-go/kubernetes"
       "k8s.io/client-go/tools/clientcmd"
       "k8s.io/klog/v2"
       ctrl "sigs.k8s.io/controller-runtime"
)

var (
       masterURL  string
       kubeconfig string
)

func main() {
       klog.InitFlags(nil)
       // Renamed to "kube-config" to avoid collision with "kubeconfig" flag defined by dependencies.
       flag.StringVar(&kubeconfig, "kube-config", "", "Path to a kubeconfig. Only required if out-of-cluster.")
       flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig.")
       flag.Parse()

       ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
       defer stop()

       cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
       if err != nil {
               klog.Fatalf("Error building kubeconfig: %s", err.Error())
       }

       // Create standard Kubernetes clientset
       kubeClient, err := kubernetes.NewForConfig(cfg)
       if err != nil {
               klog.Fatalf("Error creating kubernetes client: %s", err.Error())
       }

       // Create TPU clientset
       tpuClient, err := tpuclientset.NewForConfig(cfg)
       if err != nil {
               klog.Fatalf("Error creating TPU client: %s", err.Error())
       }

       // Create GCE Manager
       gceManager, err := gce.NewManager(ctx)
       if err != nil {
               klog.Fatalf("Error creating GCE manager: %s", err.Error())
       }
       defer gceManager.Close()

       // Create TPU Informer Factory
       tpuInformerFactory := tpuinformers.NewSharedInformerFactory(tpuClient, 30*time.Minute)

       // Instantiate tpunodegroup.Controller
       tpuController := tpunodegroup.NewController(
               ctx,
               kubeClient,
               tpuClient,
               tpuInformerFactory.Tpu().V1alpha1().TPUNodeGroups(),
               gceManager.IGM(),
               gceManager.Instances(),
       )

       mgr, err := ctrl.NewManager(cfg, ctrl.Options{})
       if err != nil {
               klog.Fatalf("Error creating manager: %s", err.Error())
       }

       if err := tpuv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
               klog.Fatalf("Error adding scheme: %s", err.Error())
       }

       if err = (&instancetemplate.InstanceTemplateReconciler{
               Client: mgr.GetClient(),
               Scheme: mgr.GetScheme(),
       }).SetupWithManager(mgr); err != nil {
               klog.Fatalf("Error setting up controller: %s", err.Error())
       }

       // Start the Informer Factory
       tpuInformerFactory.Start(ctx.Done())

       // Run the tpunodegroup.Controller in a separate goroutine
       go func() {
               if err := tpuController.Run(ctx, 2); err != nil {
                       klog.Fatalf("Error running TPU controller: %s", err.Error())
               }
       }()

       klog.Info("Starting manager")
       if err := mgr.Start(ctx); err != nil {
               klog.Fatalf("Error running manager: %s", err.Error())
       }
}

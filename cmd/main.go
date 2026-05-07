package main

import (
       "context"
       "flag"
       "os/signal"
       "syscall"

       tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
       "gke-internal.googlesource.com/tpu-node-group/pkg/controllers/instancetemplate"
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
       flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
       flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig.")
       flag.Parse()

       ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
       defer stop()

       cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
       if err != nil {
               klog.Fatalf("Error building kubeconfig: %s", err.Error())
       }

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

       klog.Info("Starting manager")
       if err := mgr.Start(ctx); err != nil {
               klog.Fatalf("Error running manager: %s", err.Error())
       }
}

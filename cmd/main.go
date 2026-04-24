package main

import (
       "context"
       "flag"
       "os/signal"
       "syscall"
       "time"

       instancetemplate "gke-internal.googlesource.com/tpu-node-group/pkg/controllers/instancetemplate"
       clientset "gke-internal.googlesource.com/tpu-node-group/pkg/generated/clientset/versioned"
       informers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/informers/externalversions"
       "k8s.io/client-go/kubernetes"
       "k8s.io/client-go/tools/clientcmd"
       "k8s.io/klog/v2"
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

       kubeClient, err := kubernetes.NewForConfig(cfg)
       if err != nil {
               klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
       }

       tpuClient, err := clientset.NewForConfig(cfg)
       if err != nil {
               klog.Fatalf("Error building tpu clientset: %s", err.Error())
       }

       tpuInformerFactory := informers.NewSharedInformerFactory(tpuClient, time.Second*30)

       instanceTemplateController := instancetemplate.NewController(
               kubeClient,
               tpuClient,
               tpuInformerFactory.Tpu().V1alpha1().InstanceTemplates(),
       )

       tpuInformerFactory.Start(ctx.Done())

       if err = instanceTemplateController.Run(2, ctx.Done()); err != nil {
               klog.Fatalf("Error running instanceTemplateController: %s", err.Error())
       }
}

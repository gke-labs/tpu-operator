package deviceplugin

import (
	"context"
	_ "embed"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
)

const (
	DevicePluginName      = "tpu-device-plugin"
	DevicePluginNamespace = "kube-system"
)

//go:embed device_plugin.yaml
var devicePluginYAML []byte

//go:embed service_account.yaml
var serviceAccountYAML []byte

// +kubebuilder:rbac:groups="",resources=nodes,verbs=update;patch;get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes/status,verbs=update;patch;get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// BuildDevicePluginDaemonSet builds the DaemonSet for the TPU device plugin.
func BuildDevicePluginDaemonSet(group *tpuapi.TPUNodeGroup) (*appsv1.DaemonSet, error) {
	ds := &appsv1.DaemonSet{}
	err := yaml.Unmarshal(devicePluginYAML, ds)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal device plugin YAML: %w", err)
	}



	return ds, nil
}

// Reconcile ensures the TPU device plugin DaemonSet exists.
func Reconcile(ctx context.Context, kubeClientset kubernetes.Interface, group *tpuapi.TPUNodeGroup) error {
	logger := klog.FromContext(ctx)
	logger.Info("Reconciling TPU Device Plugin")

	ds, err := BuildDevicePluginDaemonSet(group)
	if err != nil {
		return fmt.Errorf("failed to build device plugin DaemonSet: %w", err)
	}

	return ensureDaemonSet(ctx, kubeClientset, ds)
}



func ensureDaemonSet(ctx context.Context, kubeClientset kubernetes.Interface, ds *appsv1.DaemonSet) error {
	logger := klog.FromContext(ctx)

	existingDS, err := kubeClientset.AppsV1().DaemonSets(ds.Namespace).Get(ctx, ds.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating TPU Device Plugin DaemonSet", "namespace", ds.Namespace, "name", ds.Name)
			_, err = kubeClientset.AppsV1().DaemonSets(ds.Namespace).Create(ctx, ds, metav1.CreateOptions{})
			return err
		}
		return fmt.Errorf("failed to get DaemonSet: %w", err)
	}

	// TODO: Handle updates if needed.
	logger.Info("TPU Device Plugin DaemonSet already exists", "namespace", existingDS.Namespace, "name", existingDS.Name)

	return nil
}

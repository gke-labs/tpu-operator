package deviceplugin

import (
	_ "embed"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed device_plugin.yaml
var devicePluginYAML []byte

// BuildDevicePluginDaemonSet builds the DaemonSet for the TPU device plugin.
func BuildDevicePluginDaemonSet(namespace string) (*appsv1.DaemonSet, error) {
	ds := &appsv1.DaemonSet{}
	err := yaml.Unmarshal(devicePluginYAML, ds)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal device plugin YAML: %w", err)
	}
	ds.Namespace = namespace
	return ds, nil
}

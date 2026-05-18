package tpunodegroup

import (
	"strings"

	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
)

// acceleratorLabelValue translates the GCE machine type into the
// specific string expected by the TPU device plugin.
func acceleratorLabelValue(group *tpuapi.TPUNodeGroup) string {
	if group.Spec.InstanceConfig == nil || group.Spec.InstanceConfig.MachineType == "" {
		return ""
	}

	machineType := group.Spec.InstanceConfig.MachineType

	// Map GCE Machine Type prefixes to the plugin's expected label values
	switch {
	case strings.HasPrefix(machineType, "tpu7x-"):
		return "tpu7x"
	case strings.HasPrefix(machineType, "tpu7-"):
		return "tpu7"
	case strings.HasPrefix(machineType, "ct6e-"):
		return "tpu-v6e-slice"
	case strings.HasPrefix(machineType, "ct5lp-"):
		if group.Spec.Topology != "" && group.Spec.TargetSizePolicyMode == tpuapi.TargetSizePolicyModeBulk {
			return "tpu-v5-lite-podslice"
		}
		return "tpu-v5-lite-device"
	case strings.HasPrefix(machineType, "ct5p-"):
		if group.Spec.Topology != "" && group.Spec.TargetSizePolicyMode == tpuapi.TargetSizePolicyModeBulk {
			return "tpu-v5-podslice"
		}
		return "tpu-v5-device"
	case strings.HasPrefix(machineType, "ct4p-"):
		if group.Spec.Topology != "" && group.Spec.TargetSizePolicyMode == tpuapi.TargetSizePolicyModeBulk {
			return "tpu-v4-podslice"
		}
		return "tpu-v4-device"
	default:
		// Fallback for unknown or improperly formatted types
		return ""
	}
}

// chipsPerNode parses the machine type suffix to determine the number of
// chips attached to the VM (e.g., "tpu7x-standard-4t" -> 4).
func chipsPerNode(group *tpuapi.TPUNodeGroup) int {
	if group.Spec.InstanceConfig == nil || group.Spec.InstanceConfig.MachineType == "" {
		return 0
	}
	machineType := group.Spec.InstanceConfig.MachineType

	// The device plugin expects this to be exactly 1, 2, 4, or 8.
	// We extract it based on standard GCE machine type suffixes.
	switch {
	case strings.HasSuffix(machineType, "-1t"):
		return 1
	case strings.HasSuffix(machineType, "-2t"):
		return 2
	case strings.HasSuffix(machineType, "-4t"):
		return 4
	case strings.HasSuffix(machineType, "-8t"):
		return 8
	default:
		// If the format is unrecognized, return 0
		return 0
	}
}

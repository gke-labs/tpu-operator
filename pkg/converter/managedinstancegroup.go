package converter

import (
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
)

// ToGCEManagedInstanceGroup converts a ManagedInstanceGroup CR to GCE API InstanceGroupManager.
// This function should remain a pure conversion function. Defaulting logic
// should be handled prior to conversion in the controller's SetDefaults phase.
func ToGCEManagedInstanceGroup(cr *tpuv1alpha1.ManagedInstanceGroup) *computepb.InstanceGroupManager {
	igm := &computepb.InstanceGroupManager{
		Name:             &cr.Name,
		InstanceTemplate: &cr.Spec.InstanceTemplate,
		TargetSize:       &cr.Spec.TargetSize,
	}

	if cr.Spec.TargetSizePolicyMode != "" {
		igm.TargetSizePolicy = &computepb.InstanceGroupManagerTargetSizePolicy{
			Mode: &cr.Spec.TargetSizePolicyMode,
		}
	}

	if cr.Spec.DefaultActionOnFailure != nil {
		igm.InstanceLifecyclePolicy = &computepb.InstanceGroupManagerInstanceLifecyclePolicy{
			DefaultActionOnFailure: cr.Spec.DefaultActionOnFailure,
		}
	}

	if cr.Spec.WorkloadPolicy != nil {
		igm.ResourcePolicies = &computepb.InstanceGroupManagerResourcePolicies{
			WorkloadPolicy: cr.Spec.WorkloadPolicy,
		}
	}

	return igm
}

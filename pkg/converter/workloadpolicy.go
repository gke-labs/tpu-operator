package converter

import (
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	tpuv1alpha1 "github.com/gke-labs/tpu-operator/pkg/apis/tpu/v1alpha1"
)

// ToGCEResourcePolicy converts a WorkloadPolicy CR to GCE API ResourcePolicy.
// This function should remain a pure conversion function. Defaulting logic
// should be handled prior to conversion in the controller's SetDefaults phase.
func ToGCEResourcePolicy(cr *tpuv1alpha1.WorkloadPolicy) *computepb.ResourcePolicy {
	wp := &computepb.ResourcePolicyWorkloadPolicy{
		AcceleratorTopology: &cr.Spec.AcceleratorTopology,
	}
	if cr.Spec.Type != nil {
		wp.Type = cr.Spec.Type
	}

	return &computepb.ResourcePolicy{
		Name:           &cr.Name,
		Region:         &cr.Spec.Region,
		WorkloadPolicy: wp,
	}
}

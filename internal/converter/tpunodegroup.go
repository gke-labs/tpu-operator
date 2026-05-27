package converter

import (
	"fmt"
	"strings"

	api "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	tpuv1alpha1 "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// ToInstanceTemplateCR converts a TPUNodeGroup to an InstanceTemplate CR.
func ToInstanceTemplateCR(tpuNodeGroup *api.TPUNodeGroup) *tpuv1alpha1.InstanceTemplate {
	if tpuNodeGroup.Spec.InstanceConfig == nil {
		return nil
	}

	template := &api.InstanceTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tpuNodeGroup.InstanceTemplateName(),
			Namespace: tpuNodeGroup.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "tpu.google.com/v1alpha1",
					Kind:               "TPUNodeGroup",
					Name:               tpuNodeGroup.Name,
					UID:                tpuNodeGroup.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: api.InstanceTemplateSpec{
			Project:        tpuNodeGroup.Spec.Project,
			InstanceConfig: *tpuNodeGroup.Spec.InstanceConfig.DeepCopy(),
		},
	}

	return template
}

// ToWorkloadPolicyCR converts a TPUNodeGroup to a WorkloadPolicy CR.
func ToWorkloadPolicyCR(tpuNodeGroup *api.TPUNodeGroup) (*api.WorkloadPolicy, error) {
	region, err := getRegion(tpuNodeGroup.Spec.NodeLocation)
	if err != nil {
		return nil, fmt.Errorf("failed to determine region for WorkloadPolicy: %w", err)
	}

	policy := &api.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tpuNodeGroup.WorkloadPolicyName(),
			Namespace: tpuNodeGroup.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "tpu.google.com/v1alpha1",
					Kind:               "TPUNodeGroup",
					Name:               tpuNodeGroup.Name,
					UID:                tpuNodeGroup.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: api.WorkloadPolicySpec{
			Project:             tpuNodeGroup.Spec.Project,
			Region:              region,
			AcceleratorTopology: tpuNodeGroup.Spec.Topology,
		},
	}

	return policy, nil
}

// getRegion extracts the region from a GCP location string (which can be a region or zone).
func getRegion(location string) (string, error) {
	parts := strings.Split(location, "-")
	switch len(parts) {
	case 2:
		return location, nil
	case 3:
		return strings.Join(parts[:2], "-"), nil
	default:
		return "", fmt.Errorf("invalid GCP location format: %q (expected region or zone)", location)
	}
}

// isZone returns true if the provided location string is formatted as a GCP zone.
func isZone(location string) bool {
	return len(strings.Split(location, "-")) == 3
}

// ToManagedInstanceGroupCR converts a TPUNodeGroup to a ManagedInstanceGroup CR.
func ToManagedInstanceGroupCR(tpuNodeGroup *api.TPUNodeGroup, template *api.InstanceTemplate, policy *api.WorkloadPolicy) *api.ManagedInstanceGroup {
	var templateURL string
	if template != nil {
		templateURL = template.Status.URI
	}

	var wp *string
	if policy != nil && policy.Status.URI != "" {
		wp = &policy.Status.URI
	}

	mig := &api.ManagedInstanceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tpuNodeGroup.ManagedInstanceGroupName(),
			Namespace: tpuNodeGroup.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "tpu.google.com/v1alpha1",
					Kind:               "TPUNodeGroup",
					Name:               tpuNodeGroup.Name,
					UID:                tpuNodeGroup.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: api.ManagedInstanceGroupSpec{
			Project:              tpuNodeGroup.Spec.Project,
			Location:             tpuNodeGroup.Spec.NodeLocation,
			InstanceTemplate:     templateURL,
			TargetSize:           tpuNodeGroup.Spec.NodeCount,
			WorkloadPolicy:       wp,
			TargetSizePolicyMode: tpuNodeGroup.Spec.TargetSizePolicyMode,
		},
	}

	return mig
}

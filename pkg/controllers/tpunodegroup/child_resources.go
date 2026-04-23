package tpunodegroup

import (
	"gke-internal.googlesource.com/tpu-node-group/pkg/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// generateInstanceTemplate generates an InstanceTemplate from a TPUNodeGroup.
// Returns nil if InstanceConfig is nil.
func generateInstanceTemplate(tpuNodeGroup *api.TPUNodeGroup) *api.InstanceTemplate {
	if tpuNodeGroup.Spec.InstanceConfig == nil {
		return nil
	}

	template := &api.InstanceTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tpuNodeGroup.Name + "-template",
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
			InstanceConfig:    *tpuNodeGroup.Spec.InstanceConfig,
			MaintenancePolicy: ptr.To(api.MaintenancePolicyTerminate),
		},
	}

	return template
}

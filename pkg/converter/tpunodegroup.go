package converter

import (
	api "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
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
			InstanceConfig: *tpuNodeGroup.Spec.InstanceConfig.DeepCopy(),
		},
	}

	return template
}

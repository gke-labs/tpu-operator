package tpunodegroup

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"gke-internal.googlesource.com/tpu-node-group/pkg/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestGenerateInstanceTemplate(t *testing.T) {
	tests := []struct {
		name         string
		tpuNodeGroup *api.TPUNodeGroup
		want         *api.InstanceTemplate
	}{
		{
			name: "nil InstanceConfig",
			tpuNodeGroup: &api.TPUNodeGroup{
				Spec: api.TPUNodeGroupSpec{},
			},
			want: nil,
		},
		{
			name: "valid InstanceConfig",
			tpuNodeGroup: &api.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group",
					Namespace: "my-namespace",
					UID:       "my-uid",
				},
				Spec: api.TPUNodeGroupSpec{
					InstanceConfig: &api.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			want: &api.InstanceTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group-template",
					Namespace: "my-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "tpu.google.com/v1alpha1",
							Kind:               "TPUNodeGroup",
							Name:               "my-tpu-group",
							UID:                "my-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: api.InstanceTemplateSpec{
					InstanceConfig: api.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
					MaintenancePolicy: ptr.To(api.MaintenancePolicyTerminate),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateInstanceTemplate(tt.tpuNodeGroup)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("generateInstanceTemplate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

package converter

import (
	"testing"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestToGCEResourcePolicy(t *testing.T) {
	tests := []struct {
		name string
		cr   *tpuv1alpha1.WorkloadPolicy
		want *computepb.ResourcePolicy
	}{
		{
			name: "complete",
			cr: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project:             "test-project",
					Region:              "us-central1",
					AcceleratorTopology: "2x2x1",
					Type:                ptr.To("HIGH_THROUGHPUT"),
				},
			},
			want: &computepb.ResourcePolicy{
				Name:   ptr.To("test-policy"),
				Region: ptr.To("us-central1"),
				WorkloadPolicy: &computepb.ResourcePolicyWorkloadPolicy{
					AcceleratorTopology: ptr.To("2x2x1"),
					Type:                ptr.To("HIGH_THROUGHPUT"),
				},
			},
		},
		{
			name: "default type",
			cr: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project:             "test-project",
					Region:              "us-central1",
					AcceleratorTopology: "2x2x1",
				},
			},
			want: &computepb.ResourcePolicy{
				Name:   ptr.To("test-policy"),
				Region: ptr.To("us-central1"),
				WorkloadPolicy: &computepb.ResourcePolicyWorkloadPolicy{
					AcceleratorTopology: ptr.To("2x2x1"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToGCEResourcePolicy(tt.cr)

			if got == nil {
				t.Fatal("Expected non-nil ResourcePolicy")
			}

			if got.Name == nil {
				t.Errorf("Name = nil, want %v", *tt.want.Name)
			} else if *got.Name != *tt.want.Name {
				t.Errorf("Name = %v, want %v", *got.Name, *tt.want.Name)
			}

			if got.Region == nil {
				t.Errorf("Region = nil, want %v", *tt.want.Region)
			} else if *got.Region != *tt.want.Region {
				t.Errorf("Region = %v, want %v", *got.Region, *tt.want.Region)
			}

			if got.WorkloadPolicy == nil {
				t.Fatal("Expected non-nil WorkloadPolicy")
			}

			if got.WorkloadPolicy.AcceleratorTopology == nil {
				t.Errorf("AcceleratorTopology = nil, want %v", *tt.want.WorkloadPolicy.AcceleratorTopology)
			} else if *got.WorkloadPolicy.AcceleratorTopology != *tt.want.WorkloadPolicy.AcceleratorTopology {
				t.Errorf("AcceleratorTopology = %v, want %v", *got.WorkloadPolicy.AcceleratorTopology, *tt.want.WorkloadPolicy.AcceleratorTopology)
			}

			if tt.want.WorkloadPolicy.Type != nil {
				if got.WorkloadPolicy.Type == nil {
					t.Errorf("Type = nil, want %v", *tt.want.WorkloadPolicy.Type)
				} else if *got.WorkloadPolicy.Type != *tt.want.WorkloadPolicy.Type {
					t.Errorf("Type = %v, want %v", *got.WorkloadPolicy.Type, *tt.want.WorkloadPolicy.Type)
				}
			} else {
				if got.WorkloadPolicy.Type != nil {
					t.Errorf("Type = %v, want nil", *got.WorkloadPolicy.Type)
				}
			}
		})
	}
}

package converter

import (
	"testing"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	tpuv1alpha1 "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestToGCEManagedInstanceGroup(t *testing.T) {
	tests := []struct {
		name string
		cr   *tpuv1alpha1.ManagedInstanceGroup
		want *computepb.InstanceGroupManager
	}{
		{
			name: "complete",
			cr: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mig",
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:                "test-project",
					Location:               "us-central1-a",
					InstanceTemplate:       "https://www.googleapis.com/compute/v1/projects/test-project/global/instanceTemplates/test-template",
					TargetSize:             4,
					WorkloadPolicy:         ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/resourcePolicies/test-policy"),
					TargetSizePolicyMode:   "BULK",
					DefaultActionOnFailure: ptr.To("DO_NOTHING"),
				},
			},
			want: &computepb.InstanceGroupManager{
				Name:             ptr.To("test-mig"),
				InstanceTemplate: ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/global/instanceTemplates/test-template"),
				TargetSize:       ptr.To(int32(4)),
				TargetSizePolicy: &computepb.InstanceGroupManagerTargetSizePolicy{
					Mode: ptr.To("BULK"),
				},
				InstanceLifecyclePolicy: &computepb.InstanceGroupManagerInstanceLifecyclePolicy{
					DefaultActionOnFailure: ptr.To("DO_NOTHING"),
				},
				ResourcePolicies: &computepb.InstanceGroupManagerResourcePolicies{
					WorkloadPolicy: ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/resourcePolicies/test-policy"),
				},
			},
		},
		{
			name: "minimal",
			cr: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mig",
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
					InstanceTemplate: "https://www.googleapis.com/compute/v1/projects/test-project/global/instanceTemplates/test-template",
					TargetSize:       4,
				},
			},
			want: &computepb.InstanceGroupManager{
				Name:             ptr.To("test-mig"),
				InstanceTemplate: ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/global/instanceTemplates/test-template"),
				TargetSize:       ptr.To(int32(4)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToGCEManagedInstanceGroup(tt.cr)

			if got == nil {
				t.Fatal("Expected non-nil InstanceGroupManager")
			}

			if got.Name == nil {
				t.Errorf("Name = nil, want %v", *tt.want.Name)
			} else if *got.Name != *tt.want.Name {
				t.Errorf("Name = %v, want %v", *got.Name, *tt.want.Name)
			}

			if got.InstanceTemplate == nil {
				t.Errorf("InstanceTemplate = nil, want %v", *tt.want.InstanceTemplate)
			} else if *got.InstanceTemplate != *tt.want.InstanceTemplate {
				t.Errorf("InstanceTemplate = %v, want %v", *got.InstanceTemplate, *tt.want.InstanceTemplate)
			}

			if got.TargetSize == nil {
				t.Errorf("TargetSize = nil, want %v", *tt.want.TargetSize)
			} else if *got.TargetSize != *tt.want.TargetSize {
				t.Errorf("TargetSize = %v, want %v", *got.TargetSize, *tt.want.TargetSize)
			}

			if tt.want.TargetSizePolicy != nil {
				if got.TargetSizePolicy == nil {
					t.Errorf("TargetSizePolicy = nil, want %v", *tt.want.TargetSizePolicy.Mode)
				} else if *got.TargetSizePolicy.Mode != *tt.want.TargetSizePolicy.Mode {
					t.Errorf("TargetSizePolicy.Mode = %v, want %v", *got.TargetSizePolicy.Mode, *tt.want.TargetSizePolicy.Mode)
				}
			} else if got.TargetSizePolicy != nil {
				t.Errorf("TargetSizePolicy = %v, want nil", got.TargetSizePolicy)
			}

			if tt.want.InstanceLifecyclePolicy != nil {
				if got.InstanceLifecyclePolicy == nil {
					t.Errorf("InstanceLifecyclePolicy = nil, want %v", *tt.want.InstanceLifecyclePolicy.DefaultActionOnFailure)
				} else if *got.InstanceLifecyclePolicy.DefaultActionOnFailure != *tt.want.InstanceLifecyclePolicy.DefaultActionOnFailure {
					t.Errorf("InstanceLifecyclePolicy.DefaultActionOnFailure = %v, want %v", *got.InstanceLifecyclePolicy.DefaultActionOnFailure, *tt.want.InstanceLifecyclePolicy.DefaultActionOnFailure)
				}
			} else if got.InstanceLifecyclePolicy != nil {
				t.Errorf("InstanceLifecyclePolicy = %v, want nil", got.InstanceLifecyclePolicy)
			}

			if tt.want.ResourcePolicies != nil {
				if got.ResourcePolicies == nil {
					t.Errorf("ResourcePolicies = nil, want %v", *tt.want.ResourcePolicies.WorkloadPolicy)
				} else if *got.ResourcePolicies.WorkloadPolicy != *tt.want.ResourcePolicies.WorkloadPolicy {
					t.Errorf("ResourcePolicies.WorkloadPolicy = %v, want %v", *got.ResourcePolicies.WorkloadPolicy, *tt.want.ResourcePolicies.WorkloadPolicy)
				}
			} else if got.ResourcePolicies != nil {
				t.Errorf("ResourcePolicies = %v, want nil", got.ResourcePolicies)
			}
		})
	}
}

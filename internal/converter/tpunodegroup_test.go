package converter

import (
	"testing"

	api "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestToInstanceTemplateCR(t *testing.T) {
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
					Project: "my-project",
					InstanceConfig: &api.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			want: &api.InstanceTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group-template-1ec0000c",
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
					Project: "my-project",
					InstanceConfig: api.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToInstanceTemplateCR(tt.tpuNodeGroup)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ToInstanceTemplateCR() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetRegion(t *testing.T) {
	tests := []struct {
		location   string
		wantRegion string
		wantErr    bool
	}{
		{location: "us-central1", wantRegion: "us-central1", wantErr: false},
		{location: "us-central1-a", wantRegion: "us-central1", wantErr: false},
		{location: "us-central1-a-b", wantRegion: "", wantErr: true},
		{location: "us", wantRegion: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.location, func(t *testing.T) {
			got, err := getRegion(tt.location)
			if (err != nil) != tt.wantErr {
				t.Errorf("getRegion(%q) error = %v, wantErr %v", tt.location, err, tt.wantErr)
				return
			}
			if got != tt.wantRegion {
				t.Errorf("getRegion(%q) = %q, want %q", tt.location, got, tt.wantRegion)
			}
		})
	}
}

func TestIsZone(t *testing.T) {
	tests := []struct {
		location string
		want     bool
	}{
		{location: "us-central1", want: false},
		{location: "us-central1-a", want: true},
		{location: "us-central1-a-b", want: false},
		{location: "us", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.location, func(t *testing.T) {
			if got := isZone(tt.location); got != tt.want {
				t.Errorf("isZone(%q) = %v, want %v", tt.location, got, tt.want)
			}
		})
	}
}

func TestToWorkloadPolicyCR(t *testing.T) {
	tests := []struct {
		name         string
		tpuNodeGroup *api.TPUNodeGroup
		want         *api.WorkloadPolicy
		wantErr      bool
	}{
		{
			name: "empty topology",
			tpuNodeGroup: &api.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group",
					Namespace: "my-namespace",
					UID:       "my-uid",
				},
				Spec: api.TPUNodeGroupSpec{
					Project:      "my-project",
					NodeLocation: "us-central1-a",
				},
			},
			want: &api.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group-policy-1ec0000c",
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
				Spec: api.WorkloadPolicySpec{
					Project:             "my-project",
					Region:              "us-central1",
					AcceleratorTopology: "",
				},
			},
			wantErr: false,
		},
		{
			name: "valid topology",
			tpuNodeGroup: &api.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group",
					Namespace: "my-namespace",
					UID:       "my-uid",
				},
				Spec: api.TPUNodeGroupSpec{
					Project:      "my-project",
					NodeLocation: "us-central1-a",
					Topology:     "2x2x1",
				},
			},
			want: &api.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group-policy-1ec0000c",
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
				Spec: api.WorkloadPolicySpec{
					Project:             "my-project",
					Region:              "us-central1",
					AcceleratorTopology: "2x2x1",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid node location",
			tpuNodeGroup: &api.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group",
					Namespace: "my-namespace",
					UID:       "my-uid",
				},
				Spec: api.TPUNodeGroupSpec{
					Project:      "my-project",
					NodeLocation: "us-central1-a-b",
					Topology:     "2x2x1",
				},
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToWorkloadPolicyCR(tt.tpuNodeGroup)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToWorkloadPolicyCR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ToWorkloadPolicyCR() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestToManagedInstanceGroupCR(t *testing.T) {
	tests := []struct {
		name         string
		tpuNodeGroup *api.TPUNodeGroup
		template     *api.InstanceTemplate
		policy       *api.WorkloadPolicy
		want         *api.ManagedInstanceGroup
	}{
		{
			name: "with workload policy",
			tpuNodeGroup: &api.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group",
					Namespace: "my-namespace",
					UID:       "my-uid",
				},
				Spec: api.TPUNodeGroupSpec{
					Project:              "my-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            4,
					TargetSizePolicyMode: "BULK",
				},
			},
			template: &api.InstanceTemplate{
				Status: api.InstanceTemplateStatus{
					URI: "https://www.googleapis.com/compute/v1/projects/my-project/global/instanceTemplates/my-template",
				},
			},
			policy: &api.WorkloadPolicy{
				Status: api.WorkloadPolicyStatus{
					URI: "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/resourcePolicies/my-policy",
				},
			},
			want: &api.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group-mig-1ec0000c",
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
				Spec: api.ManagedInstanceGroupSpec{
					Project:              "my-project",
					Location:             "us-central1-a",
					InstanceTemplate:     "https://www.googleapis.com/compute/v1/projects/my-project/global/instanceTemplates/my-template",
					TargetSize:           4,
					WorkloadPolicy:       ptr.To("https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/resourcePolicies/my-policy"),
					TargetSizePolicyMode: "BULK",
					DefaultActionOnFailure: ptr.To(api.DefaultActionOnFailureDoNothing),
				},
			},
		},
		{
			name: "without workload policy",
			tpuNodeGroup: &api.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group",
					Namespace: "my-namespace",
					UID:       "my-uid",
				},
				Spec: api.TPUNodeGroupSpec{
					Project:              "my-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            4,
					TargetSizePolicyMode: "BULK",
				},
			},
			template: &api.InstanceTemplate{
				Status: api.InstanceTemplateStatus{
					URI: "https://www.googleapis.com/compute/v1/projects/my-project/global/instanceTemplates/my-template",
				},
			},
			policy: nil,
			want: &api.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group-mig-1ec0000c",
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
				Spec: api.ManagedInstanceGroupSpec{
					Project:              "my-project",
					Location:             "us-central1-a",
					InstanceTemplate:     "https://www.googleapis.com/compute/v1/projects/my-project/global/instanceTemplates/my-template",
					TargetSize:           4,
					WorkloadPolicy:       nil,
					TargetSizePolicyMode: "BULK",
					DefaultActionOnFailure: ptr.To(api.DefaultActionOnFailureDoNothing),
				},
			},
		},
		{
			name: "with targetSizePolicyMode",
			tpuNodeGroup: &api.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group",
					Namespace: "my-namespace",
					UID:       "my-uid",
				},
				Spec: api.TPUNodeGroupSpec{
					Project:              "my-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            4,
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			template: &api.InstanceTemplate{
				Status: api.InstanceTemplateStatus{
					URI: "https://www.googleapis.com/compute/v1/projects/my-project/global/instanceTemplates/my-template",
				},
			},
			policy: nil,
			want: &api.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-tpu-group-mig-1ec0000c",
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
				Spec: api.ManagedInstanceGroupSpec{
					Project:              "my-project",
					Location:             "us-central1-a",
					InstanceTemplate:     "https://www.googleapis.com/compute/v1/projects/my-project/global/instanceTemplates/my-template",
					TargetSize:           4,
					WorkloadPolicy:       nil,
					TargetSizePolicyMode: "INDIVIDUAL",
					DefaultActionOnFailure: ptr.To(api.DefaultActionOnFailureRepair),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var templateURI string
			if tt.template != nil {
				templateURI = tt.template.Status.URI
			}
			got := ToManagedInstanceGroupCR(tt.tpuNodeGroup, templateURI, tt.policy)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ToManagedInstanceGroupCR() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

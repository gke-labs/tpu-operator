package instancetemplate

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"google.golang.org/api/compute/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestToGCEInstanceTemplate(t *testing.T) {
	cr := &tpuv1alpha1.InstanceTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-template",
		},
		Spec: tpuv1alpha1.InstanceTemplateSpec{
			InstanceConfig: tpuv1alpha1.InstanceConfig{
				MachineType:    "tpu7x-standard-4t",
				Image:          ptr.To("projects/ubuntu-os-accelerator-images/global/images/ubuntu-accel-2404-amd64-tpu-tpu7x-v20260320"),
				BootDiskSizeGB: ptr.To(int32(250)),
				DiskType:       ptr.To("pd-ssd"),
				Subnetwork:     ptr.To("default"),
				NetworkTags:    []string{"tag1", "tag2"},
				Metadata: map[string]string{
					"key1": "value1",
				},
				ServiceAccounts: []tpuv1alpha1.ServiceAccount{
					{
						Email:  "default",
						Scopes: []string{"cloud-platform"},
					},
				},
				Reservation: ptr.To("test-reservation"),
			},
			MaintenancePolicy: ptr.To("TERMINATE"),
		},
	}

	gceTemplate := ToGCEInstanceTemplate(cr)

	// Verify fields
	if gceTemplate.Name != cr.Name {
		t.Errorf("Expected Name %q, got %q", cr.Name, gceTemplate.Name)
	}

	if gceTemplate.Properties.MachineType != cr.Spec.MachineType {
		t.Errorf("Expected MachineType %q, got %q", cr.Spec.MachineType, gceTemplate.Properties.MachineType)
	}

	if len(gceTemplate.Properties.Disks) != 1 {
		t.Fatalf("Expected 1 disk, got %d", len(gceTemplate.Properties.Disks))
	}
	disk := gceTemplate.Properties.Disks[0]
	if disk.InitializeParams.SourceImage != *cr.Spec.Image {
		t.Errorf("Expected SourceImage %q, got %q", *cr.Spec.Image, disk.InitializeParams.SourceImage)
	}
	if disk.InitializeParams.DiskSizeGb != int64(*cr.Spec.BootDiskSizeGB) {
		t.Errorf("Expected DiskSizeGb %d, got %d", *cr.Spec.BootDiskSizeGB, disk.InitializeParams.DiskSizeGb)
	}
	if disk.InitializeParams.DiskType != *cr.Spec.DiskType {
		t.Errorf("Expected DiskType %q, got %q", *cr.Spec.DiskType, disk.InitializeParams.DiskType)
	}

	if gceTemplate.Properties.Scheduling.OnHostMaintenance != *cr.Spec.MaintenancePolicy {
		t.Errorf("Expected OnHostMaintenance %q, got %q", *cr.Spec.MaintenancePolicy, gceTemplate.Properties.Scheduling.OnHostMaintenance)
	}

	if len(gceTemplate.Properties.NetworkInterfaces) != 1 {
		t.Fatalf("Expected 1 network interface, got %d", len(gceTemplate.Properties.NetworkInterfaces))
	}
	if gceTemplate.Properties.NetworkInterfaces[0].Subnetwork != *cr.Spec.Subnetwork {
		t.Errorf("Expected Subnetwork %q, got %q", *cr.Spec.Subnetwork, gceTemplate.Properties.NetworkInterfaces[0].Subnetwork)
	}

	if len(gceTemplate.Properties.Metadata.Items) != 1 {
		t.Fatalf("Expected 1 metadata item, got %d", len(gceTemplate.Properties.Metadata.Items))
	}
	item := gceTemplate.Properties.Metadata.Items[0]
	if item.Key != "key1" || *item.Value != "value1" {
		t.Errorf("Expected metadata key1=value1, got %s=%s", item.Key, *item.Value)
	}

	if diff := cmp.Diff(gceTemplate.Properties.Tags.Items, cr.Spec.NetworkTags); diff != "" {
		t.Errorf("NetworkTags mismatch (-want +got):\n%s", diff)
	}

	if len(gceTemplate.Properties.ServiceAccounts) != 1 {
		t.Fatalf("Expected 1 service account, got %d", len(gceTemplate.Properties.ServiceAccounts))
	}
	sa := gceTemplate.Properties.ServiceAccounts[0]
	if sa.Email != cr.Spec.ServiceAccounts[0].Email {
		t.Errorf("Expected SA Email %q, got %q", cr.Spec.ServiceAccounts[0].Email, sa.Email)
	}
	if diff := cmp.Diff(sa.Scopes, cr.Spec.ServiceAccounts[0].Scopes); diff != "" {
		t.Errorf("SA Scopes mismatch (-want +got):\n%s", diff)
	}

	if gceTemplate.Properties.ReservationAffinity.ConsumeReservationType != "SPECIFIC_RESERVATION" {
		t.Errorf("Expected ConsumeReservationType SPECIFIC_RESERVATION, got %q", gceTemplate.Properties.ReservationAffinity.ConsumeReservationType)
	}
	if gceTemplate.Properties.ReservationAffinity.Values[0] != *cr.Spec.Reservation {
		t.Errorf("Expected Reservation Value %q, got %q", *cr.Spec.Reservation, gceTemplate.Properties.ReservationAffinity.Values[0])
	}
}

func TestBuildMetadata(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]string
		want *compute.Metadata
	}{
		{
			name: "nil map",
			m:    nil,
			want: nil,
		},
		{
			name: "empty map",
			m:    map[string]string{},
			want: nil,
		},
		{
			name: "single item",
			m:    map[string]string{"key": "value"},
			want: &compute.Metadata{
				Items: []*compute.MetadataItems{
					{Key: "key", Value: ptr.To("value")},
				},
			},
		},
		{
			name: "multiple items sorted",
			m:    map[string]string{"b": "v2", "a": "v1"},
			want: &compute.Metadata{
				Items: []*compute.MetadataItems{
					{Key: "a", Value: ptr.To("v1")},
					{Key: "b", Value: ptr.To("v2")},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMetadata(tt.m)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("buildMetadata() mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func TestBuildBootDisk(t *testing.T) {
	tests := []struct {
		name     string
		cr       *tpuv1alpha1.InstanceTemplate
		wantNil  bool
		wantDisk *compute.AttachedDisk
	}{
		{
			name: "all nil",
			cr: &tpuv1alpha1.InstanceTemplate{
				Spec: tpuv1alpha1.InstanceTemplateSpec{},
			},
			wantNil: true,
		},
		{
			name: "all set",
			cr: &tpuv1alpha1.InstanceTemplate{
				Spec: tpuv1alpha1.InstanceTemplateSpec{
					InstanceConfig: tpuv1alpha1.InstanceConfig{
						Image:          ptr.To("image"),
						BootDiskSizeGB: ptr.To(int32(100)),
						DiskType:       ptr.To("pd-ssd"),
					},
				},
			},
			wantDisk: &compute.AttachedDisk{
				Boot: true,
				InitializeParams: &compute.AttachedDiskInitializeParams{
					SourceImage: "image",
					DiskSizeGb:  100,
					DiskType:    "pd-ssd",
				},
			},
		},
		{
			name: "only image",
			cr: &tpuv1alpha1.InstanceTemplate{
				Spec: tpuv1alpha1.InstanceTemplateSpec{
					InstanceConfig: tpuv1alpha1.InstanceConfig{
						Image: ptr.To("image"),
					},
				},
			},
			wantDisk: &compute.AttachedDisk{
				Boot: true,
				InitializeParams: &compute.AttachedDiskInitializeParams{
					SourceImage: "image",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildBootDisk(tt.cr.Spec.Image, tt.cr.Spec.BootDiskSizeGB, tt.cr.Spec.DiskType)
			if tt.wantNil {
				if got != nil {
					t.Errorf("Expected nil, got %v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("Expected 1 disk, got %d", len(got))
			}
			if diff := cmp.Diff(got[0], tt.wantDisk); diff != "" {
				t.Errorf("Disk mismatch (-got +want):\n%s", diff)
			}
		})
	}
}


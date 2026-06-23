package converter

import (
	"testing"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"github.com/google/go-cmp/cmp"
	tpuv1alpha1 "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
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
						Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
					},
				},
				Reservation:       ptr.To("test-reservation"),
				ProvisioningModel: ptr.To("SPOT"),
			},
			MaintenancePolicy: ptr.To("TERMINATE"),
		},
	}

	gceTemplate := ToGCEInstanceTemplate(cr)

	var _ *computepb.InstanceTemplate = gceTemplate // Force usage of computepb

	if gceTemplate == nil {
		t.Fatal("Expected non-nil gceTemplate")
	}

	if gceTemplate.Name == nil {
		t.Fatal("Expected Name to be set")
	}
	if *gceTemplate.Name != cr.Name {
		t.Errorf("Expected Name %q, got %q", cr.Name, *gceTemplate.Name)
	}

	if gceTemplate.Properties == nil {
		t.Fatal("Expected Properties to be set")
	}
	if gceTemplate.Properties.MachineType == nil {
		t.Fatal("Expected MachineType to be set")
	}
	if *gceTemplate.Properties.MachineType != cr.Spec.MachineType {
		t.Errorf("Expected MachineType %q, got %q", cr.Spec.MachineType, *gceTemplate.Properties.MachineType)
	}

	if gceTemplate.Properties.Metadata == nil {
		t.Fatal("Expected Metadata to be set")
	}
	if len(gceTemplate.Properties.Metadata.Items) != 1 {
		t.Fatalf("Expected 1 metadata item, got %d", len(gceTemplate.Properties.Metadata.Items))
	}
	item := gceTemplate.Properties.Metadata.Items[0]
	if item.Key == nil || *item.Key != "key1" {
		t.Errorf("Expected metadata key %q, got %q", "key1", *item.Key)
	}
	if item.Value == nil || *item.Value != "value1" {
		t.Errorf("Expected metadata value %q, got %q", "value1", *item.Value)
	}

	if len(gceTemplate.Properties.Disks) != 1 {
		t.Fatalf("Expected 1 disk, got %d", len(gceTemplate.Properties.Disks))
	}
	disk := gceTemplate.Properties.Disks[0]
	if disk.AutoDelete == nil || !*disk.AutoDelete {
		t.Errorf("Expected AutoDelete to be true, got %v", disk.AutoDelete)
	}
	if disk.InitializeParams == nil {
		t.Fatal("Expected InitializeParams to be set")
	}
	if disk.InitializeParams.SourceImage == nil || *disk.InitializeParams.SourceImage != *cr.Spec.Image {
		t.Errorf("Expected SourceImage %q, got %q", *cr.Spec.Image, *disk.InitializeParams.SourceImage)
	}
	if disk.InitializeParams.DiskSizeGb == nil || *disk.InitializeParams.DiskSizeGb != int64(*cr.Spec.BootDiskSizeGB) {
		t.Errorf("Expected DiskSizeGb %d, got %d", *cr.Spec.BootDiskSizeGB, *disk.InitializeParams.DiskSizeGb)
	}
	if disk.InitializeParams.DiskType == nil || *disk.InitializeParams.DiskType != *cr.Spec.DiskType {
		t.Errorf("Expected DiskType %q, got %q", *cr.Spec.DiskType, *disk.InitializeParams.DiskType)
	}

	if gceTemplate.Properties.Scheduling == nil {
		t.Fatal("Expected Scheduling to be set")
	}
	if gceTemplate.Properties.Scheduling.OnHostMaintenance == nil || *gceTemplate.Properties.Scheduling.OnHostMaintenance != *cr.Spec.MaintenancePolicy {
		t.Errorf("Expected OnHostMaintenance %q, got %q", *cr.Spec.MaintenancePolicy, *gceTemplate.Properties.Scheduling.OnHostMaintenance)
	}
	if gceTemplate.Properties.Scheduling.ProvisioningModel == nil || *gceTemplate.Properties.Scheduling.ProvisioningModel != *cr.Spec.ProvisioningModel {
		t.Errorf("Expected ProvisioningModel %q, got %q", *cr.Spec.ProvisioningModel, *gceTemplate.Properties.Scheduling.ProvisioningModel)
	}
	if gceTemplate.Properties.Scheduling.InstanceTerminationAction == nil || *gceTemplate.Properties.Scheduling.InstanceTerminationAction != "STOP" {
		t.Errorf("Expected InstanceTerminationAction %q, got %q", "STOP", *gceTemplate.Properties.Scheduling.InstanceTerminationAction)
	}

	if gceTemplate.Properties.ReservationAffinity == nil {
		t.Fatal("Expected ReservationAffinity to be set")
	}
	if gceTemplate.Properties.ReservationAffinity.ConsumeReservationType == nil || *gceTemplate.Properties.ReservationAffinity.ConsumeReservationType != "SPECIFIC_RESERVATION" {
		t.Errorf("Expected ConsumeReservationType SPECIFIC_RESERVATION, got %q", *gceTemplate.Properties.ReservationAffinity.ConsumeReservationType)
	}
	if len(gceTemplate.Properties.ReservationAffinity.Values) != 1 || gceTemplate.Properties.ReservationAffinity.Values[0] != *cr.Spec.Reservation {
		t.Errorf("Expected Reservation Value %q, got %q", *cr.Spec.Reservation, gceTemplate.Properties.ReservationAffinity.Values[0])
	}

	if len(gceTemplate.Properties.NetworkInterfaces) != 1 {
		t.Fatalf("Expected 1 network interface, got %d", len(gceTemplate.Properties.NetworkInterfaces))
	}
	if gceTemplate.Properties.NetworkInterfaces[0].Subnetwork == nil || *gceTemplate.Properties.NetworkInterfaces[0].Subnetwork != *cr.Spec.Subnetwork {
		t.Errorf("Expected Subnetwork %q, got %q", *cr.Spec.Subnetwork, *gceTemplate.Properties.NetworkInterfaces[0].Subnetwork)
	}

	if gceTemplate.Properties.Tags == nil {
		t.Fatal("Expected Tags to be set")
	}
	if diff := cmp.Diff(gceTemplate.Properties.Tags.Items, cr.Spec.NetworkTags); diff != "" {
		t.Errorf("NetworkTags mismatch (-want +got):\n%s", diff)
	}

	if len(gceTemplate.Properties.ServiceAccounts) != 1 {
		t.Fatalf("Expected 1 service account, got %d", len(gceTemplate.Properties.ServiceAccounts))
	}
	sa := gceTemplate.Properties.ServiceAccounts[0]
	if sa.Email == nil || *sa.Email != cr.Spec.ServiceAccounts[0].Email {
		t.Errorf("Expected SA Email %q, got %q", cr.Spec.ServiceAccounts[0].Email, *sa.Email)
	}
	if diff := cmp.Diff(sa.Scopes, cr.Spec.ServiceAccounts[0].Scopes); diff != "" {
		t.Errorf("SA Scopes mismatch (-want +got):\n%s", diff)
	}

	// TODO: Restore and update assertions in later cycles
	/*
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
	*/
}

// TODO: Restore and update these tests when functions are re-implemented
/*
func TestBuildMetadata(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]string
		want *computepb.Metadata
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
			want: &computepb.Metadata{
				Items: []*computepb.MetadataItems{
					{Key: "key", Value: ptr.To("value")},
				},
			},
		},
		{
			name: "multiple items sorted",
			m:    map[string]string{"b": "v2", "a": "v1"},
			want: &computepb.Metadata{
				Items: []*computepb.MetadataItems{
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
*/

/*
func TestBuildBootDisk(t *testing.T) {
	tests := []struct {
		name     string
		cr       *tpuv1alpha1.InstanceTemplate
		wantNil  bool
		wantDisk *computepb.AttachedDisk
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
			wantDisk: &computepb.AttachedDisk{
				Boot: true,
				InitializeParams: &computepb.AttachedDiskInitializeParams{
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
			wantDisk: &computepb.AttachedDisk{
				Boot: true,
				InitializeParams: &computepb.AttachedDiskInitializeParams{
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
*/

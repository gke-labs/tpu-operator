package instancetemplate

import (
	"sort"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"google.golang.org/api/compute/v1"
)

const (
	consumeReservationTypeSpecificReservation = "SPECIFIC_RESERVATION"
	reservationNameKey                     = "compute.googleapis.com/reservation-name"
)


// ToGCEInstanceTemplate converts an InstanceTemplate CR to GCE API InstanceTemplate.
func ToGCEInstanceTemplate(cr *tpuv1alpha1.InstanceTemplate) *compute.InstanceTemplate {
	properties := &compute.InstanceProperties{
		MachineType: cr.Spec.MachineType,
	}

	// Disks
	properties.Disks = buildBootDisk(cr.Spec.Image, cr.Spec.BootDiskSizeGB, cr.Spec.DiskType)

	// Scheduling
	if cr.Spec.MaintenancePolicy != nil || cr.Spec.ProvisioningModel != nil {
		scheduling := &compute.Scheduling{}
		if cr.Spec.MaintenancePolicy != nil {
			scheduling.OnHostMaintenance = *cr.Spec.MaintenancePolicy
		}
		if cr.Spec.ProvisioningModel != nil {
			scheduling.ProvisioningModel = *cr.Spec.ProvisioningModel
		}
		properties.Scheduling = scheduling
	}

	// Network Interfaces
	if cr.Spec.Subnetwork != nil {
		properties.NetworkInterfaces = []*compute.NetworkInterface{
			{
				Subnetwork: *cr.Spec.Subnetwork,
			},
		}
	}

	// Metadata
	properties.Metadata = buildMetadata(cr.Spec.Metadata)

	// Tags
	if len(cr.Spec.NetworkTags) > 0 {
		properties.Tags = &compute.Tags{
			Items: cr.Spec.NetworkTags,
		}
	}

	// Service Accounts
	if len(cr.Spec.ServiceAccounts) > 0 {
		var svcAccounts []*compute.ServiceAccount
		for _, sa := range cr.Spec.ServiceAccounts {
			svcAccounts = append(svcAccounts, &compute.ServiceAccount{
				Email:  sa.Email,
				Scopes: sa.Scopes,
			})
		}
		properties.ServiceAccounts = svcAccounts
	}

	// Reservation Affinity
	if cr.Spec.Reservation != nil {
		properties.ReservationAffinity = &compute.ReservationAffinity{
			ConsumeReservationType: consumeReservationTypeSpecificReservation,
			Key:                    reservationNameKey,
			Values:                 []string{*cr.Spec.Reservation},
		}
	}

	return &compute.InstanceTemplate{
		Name:       cr.Name,
		Properties: properties,
	}
}

// buildMetadata constructs the metadata for GCE API, sorting keys for determinism.
func buildMetadata(m map[string]string) *compute.Metadata {
	if len(m) == 0 {
		return nil
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	items := make([]*compute.MetadataItems, 0, len(m))
	for _, k := range keys {
		v := m[k]
		items = append(items, &compute.MetadataItems{
			Key:   k,
			Value: &v,
		})
	}
	return &compute.Metadata{Items: items}
}

// buildBootDisk constructs the disks slice for GCE API.
func buildBootDisk(image *string, bootDiskSizeGB *int32, diskType *string) []*compute.AttachedDisk {
	if image == nil && bootDiskSizeGB == nil && diskType == nil {
		return nil
	}

	disk := &compute.AttachedDisk{
		Boot: true,
		InitializeParams: &compute.AttachedDiskInitializeParams{},
	}
	if image != nil {
		disk.InitializeParams.SourceImage = *image
	}
	if bootDiskSizeGB != nil {
		disk.InitializeParams.DiskSizeGb = int64(*bootDiskSizeGB)
	}
	if diskType != nil {
		disk.InitializeParams.DiskType = *diskType
	}
	return []*compute.AttachedDisk{disk}
}

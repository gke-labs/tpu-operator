package converter

import (
	"sort"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	tpuv1alpha1 "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"k8s.io/utils/ptr"
)

const (
	consumeReservationTypeSpecificReservation = "SPECIFIC_RESERVATION"
	reservationNameKey                        = "compute.googleapis.com/reservation-name"
)

// ToGCEInstanceTemplate converts an InstanceTemplate CR to GCE API InstanceTemplate.
// This function should remain a pure conversion function. Defaulting logic (such as
// populating unset fields) should be handled prior to conversion in the controller's SetDefaults phase.
func ToGCEInstanceTemplate(cr *tpuv1alpha1.InstanceTemplate) *computepb.InstanceTemplate {
	properties := &computepb.InstanceProperties{
		MachineType: &cr.Spec.MachineType,
		Metadata:    buildMetadata(cr.Spec.Metadata),
		Disks:       buildBootDisk(cr.Spec.Image, cr.Spec.BootDiskSizeGB, cr.Spec.DiskType),
	}

	// Scheduling
	if cr.Spec.MaintenancePolicy != nil || cr.Spec.ProvisioningModel != nil {
		scheduling := &computepb.Scheduling{}
		if cr.Spec.MaintenancePolicy != nil {
			scheduling.OnHostMaintenance = cr.Spec.MaintenancePolicy
		}
		if cr.Spec.ProvisioningModel != nil {
			scheduling.ProvisioningModel = cr.Spec.ProvisioningModel
			// TODO: This is a shortcut to avoid GCE error when provisioning model is RESERVATION_BOUND or SPOT.
			// Proper fix should introduce a new field in InstanceTemplateSpec to track terminate action and do proper defaulting in controller.
			switch *cr.Spec.ProvisioningModel {
			case "RESERVATION_BOUND":
				scheduling.InstanceTerminationAction = ptr.To("DELETE")
			case "SPOT":
				scheduling.InstanceTerminationAction = ptr.To("STOP")
			}
		}
		properties.Scheduling = scheduling
	}

	// Reservation Affinity
	if cr.Spec.Reservation != nil {
		properties.ReservationAffinity = &computepb.ReservationAffinity{
			ConsumeReservationType: ptr.To(consumeReservationTypeSpecificReservation),
			Key:                    ptr.To(reservationNameKey),
			Values:                 []string{*cr.Spec.Reservation},
		}
	}

	// Network Interfaces
	if cr.Spec.Subnetwork != nil {
		properties.NetworkInterfaces = []*computepb.NetworkInterface{
			{
				Subnetwork: cr.Spec.Subnetwork,
			},
		}
	}

	// Tags
	if len(cr.Spec.NetworkTags) > 0 {
		properties.Tags = &computepb.Tags{
			Items: cr.Spec.NetworkTags,
		}
	}

	// Service Accounts
	if len(cr.Spec.ServiceAccounts) > 0 {
		var svcAccounts []*computepb.ServiceAccount
		for _, sa := range cr.Spec.ServiceAccounts {
			saEmail := sa.Email
			svcAccounts = append(svcAccounts, &computepb.ServiceAccount{
				Email:  &saEmail,
				Scopes: sa.Scopes,
			})
		}
		properties.ServiceAccounts = svcAccounts
	}

	return &computepb.InstanceTemplate{
		Name:       &cr.Name,
		Properties: properties,
	}
}

// buildMetadata constructs the metadata for GCE API, sorting keys for determinism.
func buildMetadata(m map[string]string) *computepb.Metadata {
	if len(m) == 0 {
		return nil
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	items := make([]*computepb.Items, 0, len(m))
	for _, k := range keys {
		v := m[k]
		key := k
		val := v
		items = append(items, &computepb.Items{
			Key:   &key,
			Value: &val,
		})
	}
	return &computepb.Metadata{Items: items}
}

// buildBootDisk constructs the disks slice for GCE API.
func buildBootDisk(image *string, bootDiskSizeGB *int32, diskType *string) []*computepb.AttachedDisk {
	if image == nil && bootDiskSizeGB == nil && diskType == nil {
		return nil
	}

	disk := &computepb.AttachedDisk{
		Boot:             ptr.To(true),
		InitializeParams: &computepb.AttachedDiskInitializeParams{},
	}
	if image != nil {
		disk.InitializeParams.SourceImage = image
	}
	if bootDiskSizeGB != nil {
		size := int64(*bootDiskSizeGB)
		disk.InitializeParams.DiskSizeGb = &size
	}
	if diskType != nil {
		disk.InitializeParams.DiskType = diskType
	}
	return []*computepb.AttachedDisk{disk}
}

package tpunodegroup

import (
	"fmt"
	"regexp"

	"cloud.google.com/go/compute/apiv1/computepb"
)

const (
	ProvisioningModelSpot             = "SPOT"
	ProvisioningModelReservationBound = "RESERVATION_BOUND"
	ProvisioningModelStandard         = "STANDARD"

	MaintenancePolicyTerminate = "TERMINATE"

	TerminationActionDelete = "DELETE"
	TerminationActionStop   = "STOP"
)

var machineTypeRegex = regexp.MustCompile(`^(tpu7x-|tpu7-|ct6e-).*`)

// ValidateExternalInstanceTemplate validates that the external instance template conforms to TPU requirements.
func ValidateExternalInstanceTemplate(computeTemplate *computepb.InstanceTemplate) error {
	machineType := extractShortName(computeTemplate.GetProperties().GetMachineType())
	if !machineTypeRegex.MatchString(machineType) {
		return fmt.Errorf("invalid external template: machine type %q is not supported", computeTemplate.GetProperties().GetMachineType())
	}

	scheduling := computeTemplate.GetProperties().GetScheduling()
	if scheduling == nil {
		return fmt.Errorf("invalid external template: scheduling properties must be specified")
	}
	provisioningModel := scheduling.GetProvisioningModel()
	if provisioningModel != ProvisioningModelSpot && provisioningModel != ProvisioningModelReservationBound && provisioningModel != ProvisioningModelStandard && provisioningModel != "" {
		return fmt.Errorf("invalid external template: provisioning model %q is not supported", provisioningModel)
	}
	if scheduling.GetOnHostMaintenance() != MaintenancePolicyTerminate {
		return fmt.Errorf("invalid external template: maintenance policy must be %s", MaintenancePolicyTerminate)
	}
	if provisioningModel == ProvisioningModelReservationBound && scheduling.GetInstanceTerminationAction() != TerminationActionDelete {
		return fmt.Errorf("invalid external template: instance termination action must be %s when provisioning model is %s", TerminationActionDelete, ProvisioningModelReservationBound)
	}
	if provisioningModel == ProvisioningModelSpot && scheduling.GetInstanceTerminationAction() != TerminationActionStop {
		return fmt.Errorf("invalid external template: instance termination action must be %s when provisioning model is %s", TerminationActionStop, ProvisioningModelSpot)
	}
	return nil
}

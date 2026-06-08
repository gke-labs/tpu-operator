package tpunodegroup

import (
	"context"
	"fmt"
	"regexp"

	"github.com/gke-labs/tpu-operator/internal/gce"
)

const (
	ProvisioningModelSpot             = "SPOT"
	ProvisioningModelReservationBound = "RESERVATION_BOUND"
	ProvisioningModelStandard         = "STANDARD"

	MaintenancePolicyTerminate = "TERMINATE"

	TerminationActionDelete = "DELETE"
	TerminationActionStop   = "STOP"
)

var instanceTemplateURIRegex = regexp.MustCompile(`^projects/([^/]+)/(locations/[^/]+|regions/[^/]+|global)/instanceTemplates/([a-z0-9-]+)$`)
var machineTypeRegex = regexp.MustCompile(`^(tpu7x-|tpu7-|ct6e-).*`)

// ValidateExternalInstanceTemplate validates that the external instance template conforms to TPU requirements.
func ValidateExternalInstanceTemplate(ctx context.Context, templateClient gce.InstanceTemplateClient, uri string) error {
	matches := instanceTemplateURIRegex.FindStringSubmatch(uri)
	if len(matches) == 0 {
		return fmt.Errorf("invalid instance template URI format")
	}
	templateProject := matches[1]
	templateName := matches[3]

	computeTemplate, err := templateClient.Get(ctx, templateProject, templateName)
	if err != nil {
		return fmt.Errorf("fetching external instance template: %w", err)
	}

	machineType := computeTemplate.GetProperties().GetMachineType()
	if !machineTypeRegex.MatchString(machineType) {
		return fmt.Errorf("invalid external template: machine type %q is not supported", machineType)
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

package tpunodegroup

import (
	"strings"
	"testing"

	"cloud.google.com/go/compute/apiv1/computepb"
	"k8s.io/utils/ptr"
)

func TestValidateExternalInstanceTemplate(t *testing.T) {
	tests := []struct {
		name        string
		template    *computepb.InstanceTemplate
		wantErr     bool
		errContains string
	}{
		{
			name: "unsupported machine type",
			template: &computepb.InstanceTemplate{
				Properties: &computepb.InstanceProperties{
					MachineType: ptr.To("n1-standard-1"),
				},
			},
			wantErr:     true,
			errContains: "machine type \"n1-standard-1\" is not supported",
		},
		{
			name: "missing scheduling properties",
			template: &computepb.InstanceTemplate{
				Properties: &computepb.InstanceProperties{
					MachineType: ptr.To("tpu7x-standard-4t"),
					Scheduling:  nil,
				},
			},
			wantErr:     true,
			errContains: "scheduling properties must be specified",
		},
		{
			name: "unsupported provisioning model",
			template: &computepb.InstanceTemplate{
				Properties: &computepb.InstanceProperties{
					MachineType: ptr.To("tpu7x-standard-4t"),
					Scheduling: &computepb.Scheduling{
						ProvisioningModel: ptr.To("UNKNOWN_MODEL"),
					},
				},
			},
			wantErr:     true,
			errContains: "provisioning model \"UNKNOWN_MODEL\" is not supported",
		},
		{
			name: "invalid maintenance policy",
			template: &computepb.InstanceTemplate{
				Properties: &computepb.InstanceProperties{
					MachineType: ptr.To("tpu7x-standard-4t"),
					Scheduling: &computepb.Scheduling{
						OnHostMaintenance: ptr.To("MIGRATE"),
					},
				},
			},
			wantErr:     true,
			errContains: "maintenance policy must be TERMINATE",
		},
		{
			name: "invalid termination action for RESERVATION_BOUND",
			template: &computepb.InstanceTemplate{
				Properties: &computepb.InstanceProperties{
					MachineType: ptr.To("tpu7x-standard-4t"),
					Scheduling: &computepb.Scheduling{
						ProvisioningModel:         ptr.To("RESERVATION_BOUND"),
						OnHostMaintenance:         ptr.To("TERMINATE"),
						InstanceTerminationAction: ptr.To("STOP"),
					},
				},
			},
			wantErr:     true,
			errContains: "instance termination action must be DELETE when provisioning model is RESERVATION_BOUND",
		},
		{
			name: "invalid termination action for SPOT",
			template: &computepb.InstanceTemplate{
				Properties: &computepb.InstanceProperties{
					MachineType: ptr.To("tpu7x-standard-4t"),
					Scheduling: &computepb.Scheduling{
						ProvisioningModel:         ptr.To("SPOT"),
						OnHostMaintenance:         ptr.To("TERMINATE"),
						InstanceTerminationAction: ptr.To("DELETE"),
					},
				},
			},
			wantErr:     true,
			errContains: "instance termination action must be STOP when provisioning model is SPOT",
		},
		{
			name: "valid configuration",
			template: &computepb.InstanceTemplate{
				Properties: &computepb.InstanceProperties{
					MachineType: ptr.To("ct6e-standard-4t"),
					Scheduling: &computepb.Scheduling{
						ProvisioningModel:         ptr.To("SPOT"),
						OnHostMaintenance:         ptr.To("TERMINATE"),
						InstanceTerminationAction: ptr.To("STOP"),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid configuration with machine type as relative URL",
			template: &computepb.InstanceTemplate{
				Properties: &computepb.InstanceProperties{
					MachineType: ptr.To("zones/us-central1-a/machineTypes/tpu7x-standard-4t"),
					Scheduling: &computepb.Scheduling{
						ProvisioningModel:         ptr.To("SPOT"),
						OnHostMaintenance:         ptr.To("TERMINATE"),
						InstanceTerminationAction: ptr.To("STOP"),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid configuration with machine type as full URL",
			template: &computepb.InstanceTemplate{
				Properties: &computepb.InstanceProperties{
					MachineType: ptr.To("https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a/machineTypes/ct6e-standard-4t"),
					Scheduling: &computepb.Scheduling{
						ProvisioningModel:         ptr.To("SPOT"),
						OnHostMaintenance:         ptr.To("TERMINATE"),
						InstanceTerminationAction: ptr.To("STOP"),
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExternalInstanceTemplate(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateExternalInstanceTemplate() = %v, wantErr %t", err, tt.wantErr)
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateExternalInstanceTemplate() got error %q, want to contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}

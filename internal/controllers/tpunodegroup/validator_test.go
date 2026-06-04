package tpunodegroup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/gke-labs/tpu-operator/internal/gce"
	"k8s.io/utils/ptr"
)

func TestValidateExternalInstanceTemplate(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		setupMock   func(tmpl *gce.MockInstanceTemplateClient)
		wantErr     bool
		errContains string
	}{
		{
			name:        "invalid URI format",
			uri:         "projects/my-project/instanceTemplates/my-template",
			setupMock:   func(tmpl *gce.MockInstanceTemplateClient) {},
			wantErr:     true,
			errContains: "invalid instance template URI format",
		},
		{
			name: "client get error",
			uri:  "projects/my-project/locations/global/instanceTemplates/my-template",
			setupMock: func(tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return nil, errors.New("api error")
				}
			},
			wantErr:     true,
			errContains: "fetching external instance template",
		},
		{
			name: "unsupported machine type",
			uri:  "projects/my-project/locations/global/instanceTemplates/my-template",
			setupMock: func(tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("n1-standard-1"),
						},
					}, nil
				}
			},
			wantErr:     true,
			errContains: "machine type \"n1-standard-1\" is not supported",
		},
		{
			name: "missing scheduling properties",
			uri:  "projects/my-project/locations/global/instanceTemplates/my-template",
			setupMock: func(tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("tpu7x-standard-4t"),
							Scheduling:  nil,
						},
					}, nil
				}
			},
			wantErr:     true,
			errContains: "scheduling properties must be specified",
		},
		{
			name: "unsupported provisioning model",
			uri:  "projects/my-project/locations/global/instanceTemplates/my-template",
			setupMock: func(tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("tpu7x-standard-4t"),
							Scheduling: &computepb.Scheduling{
								ProvisioningModel: ptr.To("UNKNOWN_MODEL"),
							},
						},
					}, nil
				}
			},
			wantErr:     true,
			errContains: "provisioning model \"UNKNOWN_MODEL\" is not supported",
		},
		{
			name: "invalid maintenance policy",
			uri:  "projects/my-project/locations/global/instanceTemplates/my-template",
			setupMock: func(tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("tpu7x-standard-4t"),
							Scheduling: &computepb.Scheduling{
								OnHostMaintenance: ptr.To("MIGRATE"),
							},
						},
					}, nil
				}
			},
			wantErr:     true,
			errContains: "maintenance policy must be TERMINATE",
		},
		{
			name: "invalid termination action for RESERVATION_BOUND",
			uri:  "projects/my-project/locations/global/instanceTemplates/my-template",
			setupMock: func(tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("tpu7x-standard-4t"),
							Scheduling: &computepb.Scheduling{
								ProvisioningModel:         ptr.To("RESERVATION_BOUND"),
								OnHostMaintenance:         ptr.To("TERMINATE"),
								InstanceTerminationAction: ptr.To("STOP"),
							},
						},
					}, nil
				}
			},
			wantErr:     true,
			errContains: "instance termination action must be DELETE when provisioning model is RESERVATION_BOUND",
		},
		{
			name: "invalid termination action for SPOT",
			uri:  "projects/my-project/locations/global/instanceTemplates/my-template",
			setupMock: func(tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("tpu7x-standard-4t"),
							Scheduling: &computepb.Scheduling{
								ProvisioningModel:         ptr.To("SPOT"),
								OnHostMaintenance:         ptr.To("TERMINATE"),
								InstanceTerminationAction: ptr.To("DELETE"),
							},
						},
					}, nil
				}
			},
			wantErr:     true,
			errContains: "instance termination action must be STOP when provisioning model is SPOT",
		},
		{
			name: "valid configuration",
			uri:  "projects/my-project/locations/global/instanceTemplates/my-template",
			setupMock: func(tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("ct6e-standard-4t"),
							Scheduling: &computepb.Scheduling{
								ProvisioningModel:         ptr.To("SPOT"),
								OnHostMaintenance:         ptr.To("TERMINATE"),
								InstanceTerminationAction: ptr.To("STOP"),
							},
						},
					}, nil
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &gce.MockInstanceTemplateClient{}
			tt.setupMock(mockClient)

			err := ValidateExternalInstanceTemplate(context.Background(), mockClient, tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateExternalInstanceTemplate(ctx, client, %q) = %v, wantErr %t", tt.uri, err, tt.wantErr)
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateExternalInstanceTemplate(ctx, client, %q) got error %q, want to contain %q", tt.uri, err.Error(), tt.errContains)
				}
			}
		})
	}
}

package tpunodegroup

import (
	"testing"

	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
)

func TestAcceleratorLabelValue(t *testing.T) {
	tests := []struct {
		name                 string
		machineType          string
		topology             string
		targetSizePolicyMode string
		want                 string
	}{
		{
			name:                 "tpu7x",
			machineType:          "tpu7x-standard-4t",
			topology:             "2x2x1",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
			want:                 "tpu7x",
		},
		{
			name:                 "ct6e",
			machineType:          "ct6e-standard-4t",
			topology:             "2x2x1",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
			want:                 "tpu-v6e-slice",
		},
		{
			name:                 "ct5lp-device",
			machineType:          "ct5lp-hightpu-4t",
			topology:             "2x2x1",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
			want:                 "tpu-v5-lite-device",
		},
		{
			name:                 "ct5lp-podslice",
			machineType:          "ct5lp-hightpu-4t",
			topology:             "4x4",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeBulk,
			want:                 "tpu-v5-lite-podslice",
		},
		{
			name:                 "ct5lp-podslice-single-host",
			machineType:          "ct5lp-hightpu-4t",
			topology:             "4x4",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
			want:                 "tpu-v5-lite-device",
		},
		{
			name:                 "tpu7x-relative-url",
			machineType:          "zones/us-central1-a/machineTypes/tpu7x-standard-4t",
			topology:             "2x2x1",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
			want:                 "tpu7x",
		},
		{
			name:                 "ct6e-full-url",
			machineType:          "https://www.googleapis.com/compute/v1/projects/my-proj/zones/us-central1-a/machineTypes/ct6e-standard-4t",
			topology:             "2x2x1",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
			want:                 "tpu-v6e-slice",
		},
		{
			name:                 "unknown",
			machineType:          "unknown-type",
			topology:             "2x2x1",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
			want:                 "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := acceleratorLabelValue(tt.machineType, tt.topology, string(tt.targetSizePolicyMode))
			if got != tt.want {
				t.Errorf("acceleratorLabelValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChipsPerNode(t *testing.T) {
	tests := []struct {
		name        string
		machineType string
		want        int
	}{
		{"1t", "ct5lp-hightpu-1t", 1},
		{"2t", "ct5lp-hightpu-2t", 2},
		{"4t", "ct5lp-hightpu-4t", 4},
		{"8t", "ct5lp-hightpu-8t", 8},
		{"url-4t", "zones/us-central1-a/machineTypes/ct5lp-hightpu-4t", 4},
		{"full-url-8t", "https://www.googleapis.com/compute/v1/projects/my-proj/zones/us-central1-a/machineTypes/ct5lp-hightpu-8t", 8},
		{"unknown", "ct5lp-hightpu-16t", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chipsPerNode(tt.machineType)
			if got != tt.want {
				t.Errorf("chipsPerNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

package api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TPUNodeGroupSpec defines the desired state of a TPUNodeGroup
type TPUNodeGroupSpec struct {
	// Project is the GCP project ID.
	// +required
	Project string `json:"project"`

	// NodeLocation is the GCE Zone where the nodes will be provisioned.
	// +required
	NodeLocation string `json:"nodeLocation"`

	// InstanceTemplate is the full URI of a user-provided instance template.
	// Cannot be set if InstanceConfig is provided.
	// +optional
	InstanceTemplate *string `json:"instanceTemplate,omitempty"`

	// InstanceConfig allows the controller to generate an instance template.
	// Cannot be set if InstanceTemplate is provided.
	// +optional
	InstanceConfig *InstanceConfig `json:"instanceConfig,omitempty"`

	// NodeCount is the total number of VMs desired.
	// +required
	NodeCount int32 `json:"nodeCount"`

	// MinNodeCount is the minimum required for a single-host slice.
	// +optional
	MinNodeCount int32 `json:"minNodeCount"`

	// AcceleratorConnectionMode dictates how the chips are interconnected.
	// Currently, the only valid value is static. (Immutable)
	// +required
	AcceleratorConnectionMode string `json:"acceleratorConnectionMode"`

	// Topology specifies the physical arrangement of the TPU chips.
	// Required for multi-host slices. If omitted, assumes single-host.
	// +optional
	Topology *string `json:"topology,omitempty"`

	// BootstrapKubernetes defines if and how the controller should install K8s components.
	// +optional
	BootstrapKubernetes *BootstrapConfig `json:"bootstrapKubernetes,omitempty"`
}

// InstanceConfig defines the GCE VM configuration for the nodes
type InstanceConfig struct {
	// MachineType is the GCE machine type (e.g., "tpu7x-standard-4t").
	// +required
	MachineType string `json:"machineType"`

	// ProvisioningModel specifies spot, reservation-bound
	// Defaults to reservation-bound if the reservation field is specified.
	// Defaults to on-demand if this field nor the reservation field is specified.
	// +optional
	ProvisioningModel *string `json:"provisioningModel,omitempty"`

	// Reservation is the name of the reservation to consume.
	// Required if ProvisioningModel is "reservation-bound.
	// +optional
	Reservation *string `json:"reservation,omitempty"`

	// Image is the boot disk image URI.
	// +optional
	Image *string `json:"image,omitempty"`

	// BootDiskSizeGB is the size of the boot disk in GB.
	// +optional
	BootDiskSizeGB *int32 `json:"bootDiskSizeGB,omitempty"`

	// DiskType specifies the type of the boot disk (e.g., "pd-ssd", "pd-balanced").
	// +optional
	DiskType *string `json:"diskType,omitempty"`

	// Subnetwork is the VPC subnetwork URI.
	// +optional
	Subnetwork *string `json:"subnetwork,omitempty"`

	// ServiceAccount is the GCP service account attached to the VMs.
	// +optional
	ServiceAccount *string `json:"serviceAccount,omitempty"`

	// NetworkTags are used to apply GCP firewall rules to the TPU nodes.
	// +optional
	NetworkTags []string `json:"networkTags,omitempty"`

	// Metadata allows setting custom GCE metadata.
	// +optional
	Metadata map[string]string `json:"metadata,omitempty"`
}

// BootstrapConfig defines settings for automated node bootstrapping
type BootstrapConfig struct {
	// Enabled indicates if the controller should bootstrap the nodes.
	// If omitted, the controller will not bootstrap the nodes.
	// +optional
	Enabled bool `json:"enabled"`

	// Version is the Kubernetes version to install.
	// +optional
	Version *string `json:"version,omitempty"`
}

// TPUNodeGroupStatus defines the observed state of a TPUNodeGroup
type TPUNodeGroupStatus struct {
	// TODO: Add conditions and node summary as per design
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TPUNodeGroup is the Schema for the tpunodegroups API
type TPUNodeGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TPUNodeGroupSpec   `json:"spec,omitempty"`
	Status TPUNodeGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TPUNodeGroupList contains a list of TPUNodeGroup
type TPUNodeGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TPUNodeGroup `json:"items"`
}

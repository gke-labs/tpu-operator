package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)


// TPUNodeGroupSpec defines the desired state of a TPUNodeGroup.
type TPUNodeGroupSpec struct {
	// Project is the GCP project ID.
	// +required
	// +kubebuilder:validation:Pattern="^[a-z][a-z0-9-]{4,28}[a-z0-9]$"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Project is immutable"
	Project string `json:"project"`

	// NodeLocation is the GCE Zone where the nodes will be provisioned.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="NodeLocation is immutable"
	NodeLocation string `json:"nodeLocation"`

	// TODO(b/513637092): Handle bring your own instance template URI.

	// InstanceConfig allows the controller to generate an instance template.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="InstanceConfig is immutable"
	InstanceConfig *InstanceConfig `json:"instanceConfig"`

	// NodeCount is the total number of VMs desired.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="NodeCount is immutable"
	NodeCount int32 `json:"nodeCount"`

	// MinNodeCount is the minimum required for a single-host slice.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="MinNodeCount is immutable"
	MinNodeCount int32 `json:"minNodeCount"`

	// AcceleratorConnectionMode dictates how the chips are interconnected.
	// Currently, the only valid value is static. (Immutable)
	// +required
	// +kubebuilder:validation:Enum=STATIC
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="AcceleratorConnectionMode is immutable"
	AcceleratorConnectionMode string `json:"acceleratorConnectionMode"`

	// Topology specifies the physical arrangement of the TPU chips.
	// Required for multi-host slices. If omitted, assumes single-host.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Topology is immutable"
	Topology string `json:"topology,omitempty"`

	// BootstrapKubernetes defines if and how the controller should install K8s components.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="BootstrapKubernetes is immutable"
	BootstrapKubernetes *BootstrapConfig `json:"bootstrapKubernetes,omitempty"`
}

// ServiceAccount defines the service account and scopes for the VM.
type ServiceAccount struct {
	// Email is the service account email. Use "default" to use the default compute service account.
	// +required
	Email string `json:"email"`

	// Scopes is a list of OAuth scopes for the service account.
	// +required
	Scopes []string `json:"scopes"`
}

// InstanceConfig defines the GCE VM configuration for the nodes.
type InstanceConfig struct {
	// MachineType is the GCE machine type (e.g., "tpu7x-standard-4t").
	// +required
	// +kubebuilder:validation:XValidation:rule="self.matches('^(tpu7x-|tpu7-|ct6e-).*')",message="Only v6 and v7 TPU machine types are supported"
	MachineType string `json:"machineType"`

	// ProvisioningModel specifies SPOT, RESERVATION_BOUND, or STANDARD.
	// Defaults to RESERVATION_BOUND if the reservation field is specified.
	// Defaults to STANDARD if neither this field nor the reservation field is specified.
	// +optional
	// +kubebuilder:validation:Enum=SPOT;RESERVATION_BOUND;STANDARD
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
	// +kubebuilder:default="default"
	// +optional
	Subnetwork *string `json:"subnetwork,omitempty"`

	// ServiceAccounts is a list of service accounts and their scopes.
	// Note: GCE currently only supports at most ONE service account per instance.
	// +optional
	// +kubebuilder:validation:MaxItems=1
	ServiceAccounts []ServiceAccount `json:"serviceAccounts,omitempty"`

	// NetworkTags are used to apply GCP firewall rules to the TPU nodes.
	// +optional
	NetworkTags []string `json:"networkTags,omitempty"`

	// Metadata allows setting custom GCE metadata.
	// +optional
	Metadata map[string]string `json:"metadata,omitempty"`
}

// BootstrapConfig defines settings for automated node bootstrapping.
// +kubebuilder:validation:XValidation:rule="has(self.version) && self.version == '1.31'",message="Version must be set to 1.31"
type BootstrapConfig struct {
	// Version is the Kubernetes version to install.
	// +optional
	Version *string `json:"version,omitempty"`

	// ControlPlaneIP is the IP address of the Kubernetes control plane.
	// +required
	ControlPlaneIP string `json:"controlPlaneIP"`
}

// TPUNodeGroupStatus defines the observed state of a TPUNodeGroup.
type TPUNodeGroupStatus struct {
	// Conditions represent the latest available observations of an object's current state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// NodeSummary provides a high-level count of nodes in various states.
	// +optional
	NodeSummary *NodeSummary `json:"nodeSummary,omitempty"`
}

// NodeSummary tracks the count of nodes in different states.
type NodeSummary struct {

	// Ready is the number of nodes that are ready and registered in the cluster.
	Ready int32 `json:"ready"`

	// Reconciling is the number of nodes currently being reconciled.
	Reconciling int32 `json:"reconciling"`

	// Failed is the number of nodes that failed to provision or join.
	Failed int32 `json:"failed"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TPUNodeGroup is the Schema for the tpunodegroups API.
type TPUNodeGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TPUNodeGroupSpec   `json:"spec,omitempty"`
	Status TPUNodeGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TPUNodeGroupList contains a list of TPUNodeGroup.
type TPUNodeGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TPUNodeGroup `json:"items"`
}

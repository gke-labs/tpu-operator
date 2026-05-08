package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// MaintenancePolicyTerminate specifies that the VMs should be terminated during maintenance.
	MaintenancePolicyTerminate = "TERMINATE"
)

// InstanceTemplateSpec defines the desired state of an InstanceTemplate.
type InstanceTemplateSpec struct {
	// Project is the GCP project ID.
	// +required
	Project string `json:"project"`

	// Reusing InstanceConfig fields at top level.
	InstanceConfig `json:",inline"`

	// MaintenancePolicy specifies the behavior of the VMs when their host machines undergo maintenance.
	// For TPU instance templates, this must always be set to TERMINATE.
	// +kubebuilder:validation:Enum=TERMINATE
	// +kubebuilder:default="TERMINATE"
	// +optional
	MaintenancePolicy *string `json:"maintenancePolicy,omitempty"`
}

// InstanceTemplateStatus defines the observed state of InstanceTemplate.
type InstanceTemplateStatus struct {
	// Conditions represent the latest available observations of an object's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// TemplateURI is the GCE URI of the created template.
	// +optional
	TemplateURI string `json:"templateURI,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// InstanceTemplate is the Schema for the instancetemplates API.
type InstanceTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InstanceTemplateSpec   `json:"spec,omitempty"`
	Status InstanceTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InstanceTemplateList contains a list of InstanceTemplate.
type InstanceTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InstanceTemplate `json:"items"`
}

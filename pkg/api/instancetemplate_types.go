package api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstanceTemplateSpec defines the desired state of an InstanceTemplate
type InstanceTemplateSpec struct {
	// Reusing InstanceConfig fields at top level.
	InstanceConfig `json:",inline"`

	// MaintenancePolicy specifies the behavior of the VMs when their host machines undergo maintenance.
	// For TPU instance templates, this must always be set to TERMINATE.
	// +kubebuilder:validation:Enum=TERMINATE
	// +kubebuilder:default="TERMINATE"
	// +optional
	MaintenancePolicy *string `json:"maintenancePolicy,omitempty"`
}

// +kubebuilder:object:root=true

// InstanceTemplate is the Schema for the instancetemplates API
type InstanceTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec InstanceTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// InstanceTemplateList contains a list of InstanceTemplate
type InstanceTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InstanceTemplate `json:"items"`
}

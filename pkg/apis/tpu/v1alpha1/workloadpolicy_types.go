package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// WorkloadPolicyTypeHighThroughput specifies a high throughput workload policy.
	WorkloadPolicyTypeHighThroughput = "HIGH_THROUGHPUT"
)

// WorkloadPolicySpec defines the desired state of a WorkloadPolicy.
type WorkloadPolicySpec struct {
	// Project is the GCP project ID.
	// +required
	Project string `json:"project"`

	// Region is the GCP region.
	// +required
	Region string `json:"region"`

	// AcceleratorTopology specifies the accelerator topology.
	// +required
	AcceleratorTopology string `json:"acceleratorTopology"`

	// Type specifies the workload policy type.
	// Currently, this must always be set to HIGH_THROUGHPUT.
	// +kubebuilder:validation:Enum=HIGH_THROUGHPUT
	// +kubebuilder:default="HIGH_THROUGHPUT"
	// +optional
	Type *string `json:"type,omitempty"`
}

// WorkloadPolicyStatus defines the observed state of WorkloadPolicy.
type WorkloadPolicyStatus struct {
	// Conditions represent the latest available observations of an object's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// URI is the GCE Resource Policy URI.
	// +optional
	URI string `json:"uri,omitempty"`

	// OperationName is the name of the pending GCE operation.
	// +optional
	OperationName string `json:"operationName,omitempty"`

	// OperationType is the type of the pending GCE operation (e.g., "CREATE", "DELETE").
	// +optional
	OperationType string `json:"operationType,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// WorkloadPolicy is the Schema for the workloadpolicies API.
type WorkloadPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkloadPolicySpec   `json:"spec,omitempty"`
	Status WorkloadPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkloadPolicyList contains a list of WorkloadPolicy.
type WorkloadPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkloadPolicy `json:"items"`
}

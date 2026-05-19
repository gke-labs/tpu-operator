package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// TargetSizePolicyModeBulk specifies that the MIG should be provisioned in bulk.
	TargetSizePolicyModeBulk = "BULK"

	// TargetSizePolicyModeIndividual specifies that the MIG should be provisioned individually.
	TargetSizePolicyModeIndividual = "INDIVIDUAL"

	// DefaultActionOnFailureDoNothing specifies that the MIG should not attempt to restart failed instances.
	DefaultActionOnFailureDoNothing = "DO_NOTHING"
)

// ManagedInstanceGroupSpec defines the desired state of a ManagedInstanceGroup.
type ManagedInstanceGroupSpec struct {
	// Project is the GCP project ID.
	// +required
	Project string `json:"project"`

	// Location is the GCP zone where the MIG will be created.
	// +required
	Location string `json:"location"`

	// InstanceTemplate is the URL of the instance template to use.
	// +required
	InstanceTemplate string `json:"instanceTemplate"`

	// TargetSize is the target number of running instances for the MIG.
	// +required
	TargetSize int32 `json:"targetSize"`

	// WorkloadPolicy is the URL of the workload policy to associate with the MIG.
	// +optional
	WorkloadPolicy *string `json:"workloadPolicy,omitempty"`

	// TargetSizePolicyMode specifies the mode of target size policy.
	// +kubebuilder:validation:Enum=BULK;INDIVIDUAL
	// +required
	TargetSizePolicyMode string `json:"targetSizePolicyMode"`

	// DefaultActionOnFailure specifies the action to take on failure.
	// +kubebuilder:validation:Enum=DO_NOTHING
	// +kubebuilder:default="DO_NOTHING"
	// +optional
	DefaultActionOnFailure *string `json:"defaultActionOnFailure,omitempty"`
}

// ManagedInstanceGroupStatus defines the observed state of ManagedInstanceGroup.
type ManagedInstanceGroupStatus struct {
	// Conditions represent the latest available observations of an object's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// URL is the GCE URL of the created managed instance group.
	// +optional
	URL string `json:"url,omitempty"`

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

// ManagedInstanceGroup is the Schema for the managedinstancegroups API.
type ManagedInstanceGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedInstanceGroupSpec   `json:"spec,omitempty"`
	Status ManagedInstanceGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedInstanceGroupList contains a list of ManagedInstanceGroup.
type ManagedInstanceGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedInstanceGroup `json:"items"`
}

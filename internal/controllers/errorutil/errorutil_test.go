package errorutil

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/googleapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Classification
	}{
		{
			name: "nil error",
			err:  nil,
			want: ClassificationTransient,
		},
		{
			name: "generic error",
			err:  errors.New("some error"),
			want: ClassificationTransient,
		},
		{
			name: "googleapi.Error 400 Bad Request",
			err:  &googleapi.Error{Code: http.StatusBadRequest},
			want: ClassificationTerminal,
		},
		{
			name: "googleapi.Error 403 Forbidden",
			err:  &googleapi.Error{Code: http.StatusForbidden},
			want: ClassificationTerminal,
		},
		{
			name: "googleapi.Error 404 Not Found",
			err:  &googleapi.Error{Code: http.StatusNotFound},
			want: ClassificationTerminal,
		},
		{
			name: "googleapi.Error 409 Conflict",
			err:  &googleapi.Error{Code: http.StatusConflict},
			want: ClassificationTerminal,
		},
		{
			name: "googleapi.Error 429 Too Many Requests",
			err:  &googleapi.Error{Code: http.StatusTooManyRequests},
			want: ClassificationTransient,
		},
		{
			name: "googleapi.Error 500 Internal Server Error",
			err:  &googleapi.Error{Code: http.StatusInternalServerError},
			want: ClassificationTransient,
		},
		{
			name: "googleapi.Error 503 Service Unavailable",
			err:  &googleapi.Error{Code: http.StatusServiceUnavailable},
			want: ClassificationTransient,
		},
		{
			name: "googleapi.Error 504 Gateway Timeout",
			err:  &googleapi.Error{Code: http.StatusGatewayTimeout},
			want: ClassificationTransient,
		},
		{
			name: "googleapi.Error other code",
			err:  &googleapi.Error{Code: http.StatusPaymentRequired},
			want: ClassificationTransient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.err)
			if got != tt.want {
				t.Errorf("Classify(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestTerminalFailureCondition(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       *metav1.Condition
	}{
		{
			name:       "empty conditions",
			conditions: nil,
			want:       nil,
		},
		{
			name: "no Ready condition",
			conditions: []metav1.Condition{
				{
					Type:   "SomeOtherCondition",
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonRequestRejected,
				},
			},
			want: nil,
		},
		{
			name: "Ready condition is True",
			conditions: []metav1.Condition{
				{
					Type:   v1alpha1.ConditionTypeReady,
					Status: metav1.ConditionTrue,
					Reason: "Created",
				},
			},
			want: nil,
		},
		{
			name: "Ready condition is False but with non-terminal reason",
			conditions: []metav1.Condition{
				{
					Type:    v1alpha1.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  "Provisioning",
					Message: "Waiting for resources",
				},
			},
			want: nil,
		},
		{
			name: "Ready condition is False with ReasonRequestRejected",
			conditions: []metav1.Condition{
				{
					Type:    v1alpha1.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonRequestRejected,
					Message: "Invalid configuration",
				},
			},
			want: &metav1.Condition{
				Type:    v1alpha1.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.ReasonRequestRejected,
				Message: "Invalid configuration",
			},
		},
		{
			name: "Ready condition is False with ReasonOperationFailed",
			conditions: []metav1.Condition{
				{
					Type:    v1alpha1.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonOperationFailed,
					Message: "GCE Operation failed",
				},
			},
			want: &metav1.Condition{
				Type:    v1alpha1.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.ReasonOperationFailed,
				Message: "GCE Operation failed",
			},
		},
		{
			name: "Ready condition is False with ReasonInstancesCreationFailed",
			conditions: []metav1.Condition{
				{
					Type:    v1alpha1.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonInstancesCreationFailed,
					Message: "VM creation failed",
				},
			},
			want: &metav1.Condition{
				Type:    v1alpha1.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.ReasonInstancesCreationFailed,
				Message: "VM creation failed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TerminalFailureCondition(tt.conditions)
			if diff := cmp.Diff(tt.want, got, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")); diff != "" {
				t.Errorf("TerminalFailureCondition() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetTerminalCondition(t *testing.T) {
	tests := []struct {
		name           string
		conditions     []metav1.Condition
		reason         string
		message        string
		wantUpdated    bool
		wantConditions []metav1.Condition
	}{
		{
			name:        "set terminal condition on empty list",
			conditions:  nil,
			reason:      v1alpha1.ReasonRequestRejected,
			message:     "Rejected",
			wantUpdated: true,
			wantConditions: []metav1.Condition{
				{
					Type:    v1alpha1.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonRequestRejected,
					Message: "Rejected",
				},
			},
		},
		{
			name: "set terminal condition when non-terminal failure exists",
			conditions: []metav1.Condition{
				{
					Type:    v1alpha1.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  "Provisioning",
					Message: "Waiting...",
				},
			},
			reason:      v1alpha1.ReasonOperationFailed,
			message:     "Operation failed",
			wantUpdated: true,
			wantConditions: []metav1.Condition{
				{
					Type:    v1alpha1.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonOperationFailed,
					Message: "Operation failed",
				},
			},
		},
		{
			name: "do not overwrite existing terminal condition",
			conditions: []metav1.Condition{
				{
					Type:    v1alpha1.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonRequestRejected,
					Message: "First rejected",
				},
			},
			reason:      v1alpha1.ReasonOperationFailed,
			message:     "Operation failed",
			wantUpdated: false,
			wantConditions: []metav1.Condition{
				{
					Type:    v1alpha1.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonRequestRejected,
					Message: "First rejected",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conds := append([]metav1.Condition(nil), tt.conditions...)
			gotUpdated := SetTerminalCondition(&conds, tt.reason, tt.message)
			if gotUpdated != tt.wantUpdated {
				t.Errorf("SetTerminalCondition() updated = %v, want %v", gotUpdated, tt.wantUpdated)
			}
			if diff := cmp.Diff(tt.wantConditions, conds, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")); diff != "" {
				t.Errorf("SetTerminalCondition() conditions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

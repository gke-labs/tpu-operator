package errorutil

import (
	"errors"
	"net/http"

	"github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"google.golang.org/api/googleapi"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Classification represents whether an error is transient or terminal.
type Classification string

const (
	ClassificationTransient Classification = "Transient"
	ClassificationTerminal  Classification = "Terminal"
)

// Classify determines if a GCP API error is transient or terminal.
func Classify(err error) Classification {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		switch gerr.Code {
		case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
			return ClassificationTerminal
		case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return ClassificationTransient
		}
	}
	// Default to transient for other errors (network timeouts, etc.) to be safe and retry.
	return ClassificationTransient
}

// TerminalFailureCondition returns the Ready condition if it is False and has a terminal reason, otherwise nil.
func TerminalFailureCondition(conditions []metav1.Condition) *metav1.Condition {
	cond := meta.FindStatusCondition(conditions, v1alpha1.ConditionTypeReady)
	if cond == nil || cond.Status != metav1.ConditionFalse {
		return nil
	}
	if cond.Reason == v1alpha1.ReasonRequestRejected ||
		cond.Reason == v1alpha1.ReasonOperationFailed ||
		cond.Reason == v1alpha1.ReasonInstancesCreationFailed {
		return cond
	}
	return nil
}

// SetTerminalCondition updates the Ready condition to False with a terminal reason,
// but only if it's not already set to a terminal failure.
func SetTerminalCondition(conditions *[]metav1.Condition, reason, message string) bool {
	if TerminalFailureCondition(*conditions) != nil {
		return false
	}
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               v1alpha1.ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	return true
}

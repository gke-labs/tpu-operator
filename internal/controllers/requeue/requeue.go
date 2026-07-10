// Package requeue provides utilities for standardized and jittered controller requeues.
package requeue

import (
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// LROPollInterval is the base interval for polling long-running GCE operations.
	LROPollInterval = 10 * time.Second

	// ShortRetryInterval is the base interval for retrying after non-terminal errors.
	ShortRetryInterval = 30 * time.Second

	// DriftCheckInterval is the base interval for periodic state verification.
	DriftCheckInterval = 10 * time.Minute
)

// Jittered returns a duration to prevent synchronized thundering herds.
// It spreads the interval uniformly between [d/2, 1.5d).
func Jittered(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return wait.Jitter(d/2, 2.0)
}

// InJitterRange returns true if the actual duration is within the expected
// jitter range [base/2, 3*base/2).
func InJitterRange(actual, base time.Duration) bool {
	if base <= 0 {
		return actual == 0
	}
	return actual >= base/2 && actual < 3*base/2
}

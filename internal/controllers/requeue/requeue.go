// Package requeue provides utilities for standardized and jittered controller requeues.
package requeue

import (
	"time"
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
// Currently returns the input duration exactly for deterministic refactoring.
func Jittered(d time.Duration) time.Duration {
	return d
}

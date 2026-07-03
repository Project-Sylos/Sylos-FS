// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import "time"

// FSErrorBucket classifies how an FS operation error should be handled.
type FSErrorBucket string

const (
	// FSErrorFatal — do not retry; backoff timer untouched.
	FSErrorFatal FSErrorBucket = "fatal"
	// FSErrorThrottle — retry; honor RetryAfter and inflate throttle backoff.
	FSErrorThrottle FSErrorBucket = "throttle"
	// FSErrorAmbiguous — retry with bounded allowance; throttle backoff only if behaviorally promoted.
	FSErrorAmbiguous FSErrorBucket = "ambiguous"
	// FSErrorRetryable — retry with short generic backoff; do not inflate throttle timer.
	FSErrorRetryable FSErrorBucket = "retryable"
)

// FSErrorClassification is the two-axis decision for one error episode.
type FSErrorClassification struct {
	Bucket     FSErrorBucket
	ErrorCode  string        // stable key fragment, e.g. "EIO", "429"
	RetryAfter time.Duration // >0 for explicit throttle signals
}

// Retryable reports whether the operation should be retried.
func (c FSErrorClassification) Retryable() bool {
	switch c.Bucket {
	case FSErrorThrottle, FSErrorAmbiguous, FSErrorRetryable:
		return true
	default:
		return false
	}
}

// InflateThrottleBackoff reports whether this error should extend rate-limit backoff state.
func (c FSErrorClassification) InflateThrottleBackoff() bool {
	return c.Bucket == FSErrorThrottle
}

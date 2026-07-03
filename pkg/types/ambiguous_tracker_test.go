// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
	"testing"
	"time"
)

func TestAmbiguousTrackerSuspectedThrottle(t *testing.T) {
	tr := NewAmbiguousErrorTracker(AmbiguousTrackerConfig{
		MinSamples:      4,
		BurstCount:      3,
		BurstWindow:     2 * time.Second,
		HighWorkerFloor: 4,
	})
	now := time.Now()
	for i := 0; i < 4; i++ {
		tr.Record("ListChildren", "EIO", 8, now.Add(time.Duration(i)*100*time.Millisecond))
	}
	if !tr.SuspectedThrottle("ListChildren", "EIO") {
		t.Fatal("expected suspected throttle under high concurrency burst")
	}
	if tr.SuspectedThrottle("ListChildren", "ENOENT") {
		t.Fatal("unexpected promotion for unrelated code")
	}
}

func TestAmbiguousTrackerLowConcurrencyNotPromoted(t *testing.T) {
	tr := NewAmbiguousErrorTracker(AmbiguousTrackerConfig{MinSamples: 4, BurstCount: 3})
	now := time.Now()
	for i := 0; i < 5; i++ {
		tr.Record("ListChildren", "EIO", 1, now.Add(time.Duration(i)*500*time.Millisecond))
	}
	if tr.SuspectedThrottle("ListChildren", "EIO") {
		t.Fatal("steady low-concurrency errors should not promote")
	}
}

func TestFSErrorClassificationAxes(t *testing.T) {
	throttle := FSErrorClassification{Bucket: FSErrorThrottle, RetryAfter: time.Second}
	if !throttle.Retryable() || !throttle.InflateThrottleBackoff() {
		t.Fatal("throttle bucket")
	}
	ambiguous := FSErrorClassification{Bucket: FSErrorAmbiguous}
	if !ambiguous.Retryable() || ambiguous.InflateThrottleBackoff() {
		t.Fatal("ambiguous bucket")
	}
	fatal := FSErrorClassification{Bucket: FSErrorFatal}
	if fatal.Retryable() {
		t.Fatal("fatal bucket")
	}
}

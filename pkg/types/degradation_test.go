// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
	"testing"
	"time"
)

func TestFSDegradationStateRecordAndTake(t *testing.T) {
	s := NewFSDegradationState()
	s.RecordSignal(FSDegradationSignal{
		Kind:       FSDegradationRateLimit,
		RetryAfter: 250 * time.Millisecond,
		Operation:  "ListChildren",
		At:         time.Now(),
	})
	snap := s.DegradationState()
	if snap.RecentHits != 1 {
		t.Fatalf("RecentHits = %d, want 1", snap.RecentHits)
	}
	if snap.RateLimitedUntil.IsZero() {
		t.Fatal("expected RateLimitedUntil set")
	}
	if got := s.TakeRecentHits(); got != 1 {
		t.Fatalf("TakeRecentHits = %d, want 1", got)
	}
	if got := s.TakeRecentHits(); got != 0 {
		t.Fatalf("second TakeRecentHits = %d, want 0", got)
	}
}

func TestFSDegradationReporterIncludesGetDegradationState(t *testing.T) {
	s := NewFSDegradationState()
	var r FSDegradationReporter = s
	if r.GetDegradationState() != s {
		t.Fatal("GetDegradationState must return the shared state pointer for ME rate-limit bridging")
	}
}

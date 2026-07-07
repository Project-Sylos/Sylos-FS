// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
	"testing"
	"time"
)

func TestScheduleThrottleBackoffExponential(t *testing.T) {
	s := NewFSDegradationState()
	d1 := s.ScheduleThrottleBackoff()
	d2 := s.ScheduleThrottleBackoff()
	if d2 <= d1 {
		t.Fatalf("expected increasing backoff d1=%v d2=%v", d1, d2)
	}
	if d1 < time.Second+ThrottleBackoffJitter {
		t.Fatalf("d1=%v too small", d1)
	}
	s.ClearThrottleStreak()
	d3 := s.ScheduleThrottleBackoff()
	if d3 != d1 {
		t.Fatalf("after clear want %v got %v", d1, d3)
	}
}

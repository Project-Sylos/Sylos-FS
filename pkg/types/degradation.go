// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
	"sync"
	"sync/atomic"
	"time"
)

// FSDegradationKind classifies a degradation signal from an FS adapter.
type FSDegradationKind string

const (
	FSDegradationRateLimit    FSDegradationKind = "rate_limit"
	FSDegradationSuspectedRateLimit FSDegradationKind = "suspected_rate_limit"
	FSDegradationHighLatency  FSDegradationKind = "high_latency"
	FSDegradationPacketLoss   FSDegradationKind = "packet_loss"
	FSDegradationAuthRefresh  FSDegradationKind = "auth_refresh"
)

// FSDegradationSignal is one degradation episode reported by an adapter.
type FSDegradationSignal struct {
	Kind       FSDegradationKind
	RetryAfter time.Duration // 0 if unknown
	Operation  string        // e.g. "ListChildren", "OpenRead"
	At         time.Time
}

// FSDegradationSnapshot is a point-in-time view of adapter degradation state.
type FSDegradationSnapshot struct {
	RateLimitedUntil time.Time
	RecentHits       int64
}

// FSDegradationReporter is implemented by adapters that expose degradation telemetry.
// GetDegradationState is required for ME rate-limit bridging (AIMD FS_THROTTLE + UI badge).
type FSDegradationReporter interface {
	DegradationState() FSDegradationSnapshot
	RecordSignal(FSDegradationSignal)
	GetDegradationState() *FSDegradationState
}

// FSDegradationState holds shared per-backend degradation counters.
// Attach one instance per FS backend (shared when SRC/DST use the same connection).
type FSDegradationState struct {
	mu               sync.RWMutex
	rateLimitedUntil time.Time
	recentHits       int64
	throttleStreak   int
	ambiguous        *AmbiguousErrorTracker
}

// NewFSDegradationState creates an empty degradation state tracker.
func NewFSDegradationState() *FSDegradationState {
	return &FSDegradationState{
		ambiguous: NewAmbiguousErrorTracker(AmbiguousTrackerConfig{}),
	}
}

// AmbiguousTracker returns the behavioral ambiguous-error tracker for this backend.
func (s *FSDegradationState) AmbiguousTracker() *AmbiguousErrorTracker {
	if s == nil {
		return nil
	}
	return s.ambiguous
}

// DegradationState returns the current snapshot without resetting counters.
func (s *FSDegradationState) DegradationState() FSDegradationSnapshot {
	if s == nil {
		return FSDegradationSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return FSDegradationSnapshot{
		RateLimitedUntil: s.rateLimitedUntil,
		RecentHits:       atomic.LoadInt64(&s.recentHits),
	}
}

// RecordSignal records a degradation episode.
func (s *FSDegradationState) RecordSignal(sig FSDegradationSignal) {
	if s == nil {
		return
	}
	atomic.AddInt64(&s.recentHits, 1)
	inflateUntil := sig.Kind == FSDegradationRateLimit || sig.Kind == FSDegradationSuspectedRateLimit
	if inflateUntil && sig.RetryAfter > 0 {
		until := sig.At
		if until.IsZero() {
			until = time.Now()
		}
		until = until.Add(sig.RetryAfter)
		s.mu.Lock()
		if until.After(s.rateLimitedUntil) {
			s.rateLimitedUntil = until
		}
		s.mu.Unlock()
	}
}

// ThrottleBackoffJitter is added after explicit Retry-After values from providers.
const ThrottleBackoffJitter = 250 * time.Millisecond

// MaxThrottleBackoff caps exponential throttle sleeps (Google recommends bounded backoff).
const MaxThrottleBackoff = 64 * time.Second

// ScheduleThrottleBackoff returns the next sleep duration for a throttle episode using
// exponential backoff (1s, 2s, 4s, …) plus ThrottleBackoffJitter.
func (s *FSDegradationState) ScheduleThrottleBackoff() time.Duration {
	if s == nil {
		return time.Second + ThrottleBackoffJitter
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.throttleStreak++
	n := s.throttleStreak
	if n > 6 {
		n = 6
	}
	base := time.Second << (n - 1)
	if base > MaxThrottleBackoff {
		base = MaxThrottleBackoff
	}
	return base + ThrottleBackoffJitter
}

// ClearThrottleStreak resets exponential backoff after a successful FS operation.
func (s *FSDegradationState) ClearThrottleStreak() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.throttleStreak = 0
	s.mu.Unlock()
}

// TakeRecentHits returns recent hit count and resets it (read-and-reset for observer polling).
func (s *FSDegradationState) TakeRecentHits() int64 {
	if s == nil {
		return 0
	}
	return atomic.SwapInt64(&s.recentHits, 0)
}

// GetDegradationState implements FSDegradationReporter for the state itself.
func (s *FSDegradationState) GetDegradationState() *FSDegradationState {
	return s
}

// AsDegradationReporter returns the state as FSDegradationReporter when non-nil.
func (s *FSDegradationState) AsDegradationReporter() FSDegradationReporter {
	if s == nil {
		return nil
	}
	return s
}

// DegradationReporterFrom returns a degradation reporter from an FSAdapter, if supported.
func DegradationReporterFrom(adapter FSAdapter) FSDegradationReporter {
	if adapter == nil {
		return nil
	}
	if r, ok := adapter.(FSDegradationReporter); ok {
		return r
	}
	return nil
}

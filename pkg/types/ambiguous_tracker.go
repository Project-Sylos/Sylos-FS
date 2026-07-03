// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
	"sync"
	"time"
)

// AmbiguousTrackerConfig tunes behavioral promotion for ambiguous errors.
type AmbiguousTrackerConfig struct {
	MinSamples      int           // minimum total hits before promotion (default 4)
	BurstCount      int           // events within BurstWindow to count as a burst (default 3)
	BurstWindow     time.Duration // default 2s
	HighWorkerFloor int           // worker counts >= this are "high load" (default 4)
}

// AmbiguousErrorTracker collects runtime evidence for ambiguous errno / opaque errors.
// Keyed by operation+error_code per backend group (attach to FSDegradationState).
type AmbiguousErrorTracker struct {
	mu     sync.Mutex
	cfg    AmbiguousTrackerConfig
	keys   map[string]*ambiguousKeyStats
}

type ambiguousKeyStats struct {
	total      int
	lowWorkers int // workers < HighWorkerFloor
	highWorkers int
	timestamps []time.Time
}

// NewAmbiguousErrorTracker creates a tracker with normalized defaults.
func NewAmbiguousErrorTracker(cfg AmbiguousTrackerConfig) *AmbiguousErrorTracker {
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 4
	}
	if cfg.BurstCount <= 0 {
		cfg.BurstCount = 3
	}
	if cfg.BurstWindow <= 0 {
		cfg.BurstWindow = 2 * time.Second
	}
	if cfg.HighWorkerFloor <= 0 {
		cfg.HighWorkerFloor = 4
	}
	return &AmbiguousErrorTracker{
		cfg:  cfg,
		keys: make(map[string]*ambiguousKeyStats),
	}
}

// AmbiguousKey builds a stable telemetry key for (operation, error_code).
func AmbiguousKey(operation, errorCode string) string {
	if operation == "" {
		operation = "unknown"
	}
	if errorCode == "" {
		errorCode = "unknown"
	}
	return operation + ":" + errorCode
}

// Record logs one ambiguous error occurrence at the given worker concurrency.
func (t *AmbiguousErrorTracker) Record(operation, errorCode string, workerCount int, at time.Time) {
	if t == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	key := AmbiguousKey(operation, errorCode)
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.keys[key]
	if st == nil {
		st = &ambiguousKeyStats{}
		t.keys[key] = st
	}
	st.total++
	if workerCount >= t.cfg.HighWorkerFloor {
		st.highWorkers++
	} else {
		st.lowWorkers++
	}
	st.timestamps = append(st.timestamps, at)
	const maxTS = 32
	if len(st.timestamps) > maxTS {
		st.timestamps = st.timestamps[len(st.timestamps)-maxTS:]
	}
}

// SuspectedThrottle returns true when concurrency and burst correlation suggest load shedding.
func (t *AmbiguousErrorTracker) SuspectedThrottle(operation, errorCode string) bool {
	if t == nil {
		return false
	}
	key := AmbiguousKey(operation, errorCode)
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.keys[key]
	if st == nil || st.total < t.cfg.MinSamples {
		return false
	}
	if !st.hasBurst(t.cfg.BurstCount, t.cfg.BurstWindow) {
		return false
	}
	// Throttle-like: more hits under high concurrency than low.
	if st.highWorkers < 2 {
		return false
	}
	return st.highWorkers >= st.lowWorkers+2 || (st.lowWorkers == 0 && st.highWorkers >= t.cfg.MinSamples)
}

func (st *ambiguousKeyStats) hasBurst(count int, window time.Duration) bool {
	if len(st.timestamps) < count {
		return false
	}
	ts := st.timestamps
	for i := 0; i <= len(ts)-count; i++ {
		if ts[i+count-1].Sub(ts[i]) <= window {
			return true
		}
	}
	return false
}

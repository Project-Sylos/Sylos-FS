// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import "sync/atomic"

// FSConcurrencyHint allows the engine to report active worker count for behavioral error classification.
type FSConcurrencyHint interface {
	SetActiveWorkers(n int)
}

// ConcurrencyHint tracks active worker count reported by the migration engine.
type ConcurrencyHint struct {
	activeWorkers atomic.Int32
}

// SetActiveWorkers implements FSConcurrencyHint.
func (c *ConcurrencyHint) SetActiveWorkers(n int) {
	if c == nil {
		return
	}
	if n < 0 {
		n = 0
	}
	c.activeWorkers.Store(int32(n))
}

// ActiveWorkers returns the last reported worker count.
func (c *ConcurrencyHint) ActiveWorkers() int {
	if c == nil {
		return 0
	}
	return int(c.activeWorkers.Load())
}

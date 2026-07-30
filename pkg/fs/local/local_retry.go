// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"context"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func (l *LocalFS) withClassifiedRetryCtx(ctx context.Context, operation string, op func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var tracker *types.AmbiguousErrorTracker
	if l.degradation != nil {
		tracker = l.degradation.AmbiguousTracker()
	}
	var attempt int
	// A behaviorally promoted ambiguous error reuses the throttle sleep path, so
	// OnRateLimitWait fires right after OnSuspectedThrottle. Record it once, as suspected,
	// so one local errno burst does not count as two degradation hits.
	promoted := false
	return credentials.DoWithClassifiedRetry(ctx, credentials.ClassifiedRetryConfig{
		RetryConfig: credentials.RetryConfig{
			MaxIterations:     32,
			MaxRateLimitWaits: 8,
			MaxRateLimitSleep: 5 * time.Second,
			DefaultRateLimitSleep: 250 * time.Millisecond,
			OnRateLimitWait: func(retryAfter time.Duration, attempt int) {
				if promoted {
					promoted = false
					return
				}
				l.recordDegradation(types.FSDegradationRateLimit, operation, retryAfter)
			},
		},
		Operation:         operation,
		Classify:          types.ClassifyLocalError,
		AmbiguousTracker:  tracker,
		WorkerCount:       l.ActiveWorkers,
		OnSuspectedThrottle: func(class types.FSErrorClassification, attempt int) {
			promoted = true
			l.recordDegradation(types.FSDegradationSuspectedRateLimit, operation, 250*time.Millisecond)
		},
	}, func() error {
		attempt++
		if l.injectBeforeOp != nil {
			if err := l.injectBeforeOp(operation, attempt); err != nil {
				return err
			}
		}
		return op()
	})
}

func (l *LocalFS) recordDegradation(kind types.FSDegradationKind, operation string, retryAfter time.Duration) {
	if l.degradation == nil {
		return
	}
	l.degradation.RecordSignal(types.FSDegradationSignal{
		Kind:       kind,
		RetryAfter: retryAfter,
		Operation:  operation,
		At:         time.Now(),
	})
}

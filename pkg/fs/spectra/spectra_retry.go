// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package spectra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"codeberg.org/Sylos/Spectra/sdk"
	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func (s *SpectraFS) withClassifiedRetry(ctx context.Context, operation string, op func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var tracker *types.AmbiguousErrorTracker
	if s.degradation != nil {
		tracker = s.degradation.AmbiguousTracker()
	}
	return credentials.DoWithClassifiedRetry(ctx, credentials.ClassifiedRetryConfig{
		RetryConfig: credentials.RetryConfig{
			MaxIterations:         32,
			MaxRateLimitWaits:     32,
			MaxRateLimitSleep:     2 * time.Second,
			DefaultRateLimitSleep: 100 * time.Millisecond,
			IsAuthFailure: func(err error) bool {
				if err == nil {
					return false
				}
				if _, ok := sdk.IsUnauthorized(err); ok {
					return true
				}
				return errors.Is(err, credentials.ErrNeedsRefresh)
			},
			Refresh: func(rctx context.Context) error {
				// TEMP smoke-test log — remove after auth refresh verification
				fmt.Printf("[spectra] FS middleware refreshed access token world=%s op=%s\n", s.world, operation)
				if s.fs == nil || !s.fs.AuthEnabled() {
					return nil
				}
				_, err := s.fs.EnsureWorldAuth(s.world)
				return err
			},
			OnRateLimitWait: func(retryAfter time.Duration, attempt int) {
				s.recordDegradationSignal(types.FSDegradationRateLimit, operation, retryAfter)
			},
		},
		Operation:        operation,
		Classify:         ClassifySpectraError,
		AmbiguousTracker: tracker,
		WorkerCount:      s.ActiveWorkers,
		OnSuspectedThrottle: func(class types.FSErrorClassification, attempt int) {
			s.recordDegradationSignal(types.FSDegradationSuspectedRateLimit, operation, 250*time.Millisecond)
		},
	}, op)
}

func (s *SpectraFS) recordDegradationSignal(kind types.FSDegradationKind, operation string, retryAfter time.Duration) {
	if s.degradation == nil {
		return
	}
	s.degradation.RecordSignal(types.FSDegradationSignal{
		Kind:       kind,
		RetryAfter: retryAfter,
		Operation:  operation,
		At:         time.Now(),
	})
}

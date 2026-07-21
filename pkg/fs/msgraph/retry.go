// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import (
	"context"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func doClassifiedRetry(ctx context.Context, a *AdapterOps, operation string, tracker *types.AmbiguousErrorTracker, op func() error) error {
	return credentials.DoWithClassifiedRetry(ctx, credentials.ClassifiedRetryConfig{
		RetryConfig: credentials.RetryConfig{
			MaxIterations:         8,
			MaxRateLimitWaits:     0,
			MaxRateLimitSleep:     types.MaxThrottleBackoff,
			DefaultRateLimitSleep: time.Second + types.ThrottleBackoffJitter,
			IsAuthFailure:         IsAuthFailure,
			Refresh: func(rctx context.Context) error {
				a.Auth.ClearAccessToken()
				return a.Auth.RefreshAccessToken(rctx)
			},
			OnRateLimitWait: func(retryAfter time.Duration, attempt int) {
				a.recordDegradation(types.FSDegradationRateLimit, operation, retryAfter)
			},
			OnRateLimitExhausted: func(err error) {
				a.recordDegradation(types.FSDegradationRateLimit, operation, ThrottleBackoff(err, a.Auth.Degradation()))
			},
		},
		Operation:        operation,
		Classify:         a.classifyError,
		AmbiguousTracker: tracker,
		WorkerCount:      a.ActiveWorkers,
		OnSuspectedThrottle: func(class types.FSErrorClassification, attempt int) {
			a.recordDegradation(types.FSDegradationSuspectedRateLimit, operation, 250*time.Millisecond)
		},
	}, func() error {
		err := op()
		if err == nil && a.Auth.Degradation() != nil {
			a.Auth.Degradation().ClearThrottleStreak()
		}
		return err
	})
}

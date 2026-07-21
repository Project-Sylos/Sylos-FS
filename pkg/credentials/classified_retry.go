// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package credentials

import (
	"context"
	"fmt"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// ClassifiedRetryConfig extends RetryConfig with explicit error buckets and ambiguous handling.
type ClassifiedRetryConfig struct {
	RetryConfig

	// Operation names ambiguous telemetry keys (e.g. "ListChildren").
	Operation string

	// Classify maps errors to buckets. When nil, only IsRateLimited / auth paths apply.
	Classify func(err error) types.FSErrorClassification

	// AmbiguousTracker records behavioral evidence; typically FSDegradationState.AmbiguousTracker().
	AmbiguousTracker *types.AmbiguousErrorTracker

	// WorkerCount returns active concurrency when an ambiguous error occurs.
	WorkerCount func() int

	// MaxAmbiguousRetries caps ambiguous-bucket retries (default 6).
	MaxAmbiguousRetries int

	// MaxGenericRetries caps non-throttle retryable errors (default 3).
	MaxGenericRetries int

	// GenericRetrySleep is sleep between generic retryable retries (default 50ms).
	GenericRetrySleep time.Duration

	// AmbiguousRetrySleep is sleep between ambiguous retries before promotion (default 100ms).
	AmbiguousRetrySleep time.Duration

	// OnSuspectedThrottle is called when ambiguous error is behaviorally promoted.
	OnSuspectedThrottle func(class types.FSErrorClassification, attempt int)
}

// DoWithClassifiedRetry is the FS-side middleware around a single adapter I/O call.
// Workers only wait on the FS method; they never refresh tokens themselves.
//
// Order of handling on error:
//  1. Auth failure (IsAuthFailure) → Refresh once, then retry op. If refresh fails,
//     return the refresh error (caller classifies as auth/401). LocalFS omits Refresh.
//  2. Fatal → return immediately
//  3. Explicit throttle → sleep (capped) and retry
//  4. Ambiguous → neutral retry until behavioral promotion to throttle
//  5. Generic retryable → short sleep and retry
//
// Explicit throttle (Classify or IsRateLimited) inflates rate-limit sleep; ambiguous
// errors default to neutral retry until behavioral promotion; generic retryable errors
// retry without touching throttle state.
func DoWithClassifiedRetry(ctx context.Context, cfg ClassifiedRetryConfig, op func() error) error {
	base := normalizeRetryConfig(cfg.RetryConfig)
	if cfg.MaxAmbiguousRetries <= 0 {
		cfg.MaxAmbiguousRetries = 6
	}
	if cfg.MaxGenericRetries <= 0 {
		cfg.MaxGenericRetries = 3
	}
	if cfg.GenericRetrySleep <= 0 {
		cfg.GenericRetrySleep = 50 * time.Millisecond
	}
	if cfg.AmbiguousRetrySleep <= 0 {
		cfg.AmbiguousRetrySleep = 100 * time.Millisecond
	}

	var refreshed bool
	rateWaits := 0
	ambiguousWaits := 0
	genericWaits := 0

	for i := 0; i < base.MaxIterations; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := op()
		if err == nil {
			return nil
		}

		// Auth refresh before bucket handling so 401 / auth-fatal can recover once
		// (mirrors DoWithAuthRetry). Classify may mark these Fatal or Retryable.
		if base.IsAuthFailure != nil && base.IsAuthFailure(err) && base.Refresh != nil && !refreshed {
			if rerr := base.Refresh(ctx); rerr != nil {
				return fmt.Errorf("%w (refresh failed: %v)", err, rerr)
			}
			refreshed = true
			continue
		}

		var class types.FSErrorClassification
		hasClass := false
		if cfg.Classify != nil {
			class = cfg.Classify(err)
			hasClass = true
		}

		if hasClass && class.Bucket == types.FSErrorFatal {
			return err
		}

		// Explicit throttle: Classify bucket or legacy IsRateLimited.
		if throttle, retryAfter := classifyAsThrottle(hasClass, class, err, base); throttle {
			if rateWaits >= base.MaxRateLimitWaits {
				if base.OnRateLimitExhausted != nil {
					base.OnRateLimitExhausted(err)
				}
				return err
			}
			sleep := retryAfter
			if sleep <= 0 {
				sleep = base.DefaultRateLimitSleep
			}
			if sleep > base.MaxRateLimitSleep {
				sleep = base.MaxRateLimitSleep
			}
			if base.OnRateLimitWait != nil {
				base.OnRateLimitWait(sleep, rateWaits+1)
			}
			if err := sleepCtx(ctx, sleep); err != nil {
				return err
			}
			rateWaits++
			continue
		}

		rateWaits = 0

		if hasClass && class.Bucket == types.FSErrorAmbiguous {
			if ambiguousWaits >= cfg.MaxAmbiguousRetries {
				return err
			}
			workers := 0
			if cfg.WorkerCount != nil {
				workers = cfg.WorkerCount()
			}
			if cfg.AmbiguousTracker != nil {
				cfg.AmbiguousTracker.Record(cfg.Operation, class.ErrorCode, workers, time.Now())
				if cfg.AmbiguousTracker.SuspectedThrottle(cfg.Operation, class.ErrorCode) {
					if cfg.OnSuspectedThrottle != nil {
						cfg.OnSuspectedThrottle(class, ambiguousWaits+1)
					}
					// Promoted: use throttle backoff path once.
					sleep := base.DefaultRateLimitSleep
					if sleep > base.MaxRateLimitSleep {
						sleep = base.MaxRateLimitSleep
					}
					if base.OnRateLimitWait != nil {
						base.OnRateLimitWait(sleep, 1)
					}
					if err := sleepCtx(ctx, sleep); err != nil {
						return err
					}
					ambiguousWaits++
					continue
				}
			}
			if err := sleepCtx(ctx, cfg.AmbiguousRetrySleep); err != nil {
				return err
			}
			ambiguousWaits++
			continue
		}

		if hasClass && class.Bucket == types.FSErrorRetryable {
			if genericWaits >= cfg.MaxGenericRetries {
				return err
			}
			if err := sleepCtx(ctx, cfg.GenericRetrySleep); err != nil {
				return err
			}
			genericWaits++
			continue
		}

		return err
	}
	return fmt.Errorf("credentials: DoWithClassifiedRetry exceeded MaxIterations (%d)", base.MaxIterations)
}

func classifyAsThrottle(hasClass bool, class types.FSErrorClassification, err error, base RetryConfig) (bool, time.Duration) {
	if hasClass && class.Bucket == types.FSErrorThrottle {
		return true, class.RetryAfter
	}
	if base.IsRateLimited != nil {
		if d, ok := base.IsRateLimited(err); ok {
			return true, d
		}
	}
	return false, 0
}

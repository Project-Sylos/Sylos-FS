// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package credentials

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrNeedsRefresh is a sentinel for recoverable auth failures. Cloud adapters may
	// wrap provider errors with fmt.Errorf("%w: ...", ErrNeedsRefresh) so IsAuthFailure
	// can use errors.Is(err, ErrNeedsRefresh).
	ErrNeedsRefresh = errors.New("credentials: needs token refresh")
)

// RateLimitedError indicates the provider asked the client to back off.
// Use with errors.As in IsRateLimited, or return from custom classifiers.
type RateLimitedError struct {
	RetryAfter time.Duration
	Err        error
}

func (e *RateLimitedError) Error() string {
	if e == nil {
		return "credentials: rate limited"
	}
	if e.Err != nil {
		return fmt.Sprintf("credentials: rate limited (retry after %s): %v", e.RetryAfter, e.Err)
	}
	return fmt.Sprintf("credentials: rate limited (retry after %s)", e.RetryAfter)
}

func (e *RateLimitedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RateLimited returns an error that IsRateLimitedDefault recognizes.
func RateLimited(retryAfter time.Duration) error {
	return &RateLimitedError{RetryAfter: retryAfter}
}

// RetryConfig controls DoWithAuthRetry. Zero values get safe defaults.
type RetryConfig struct {
	// Refresh runs after an auth failure (once per call) before retrying op.
	Refresh func(ctx context.Context) error

	// IsAuthFailure returns true if err should trigger Refresh (if not yet refreshed).
	IsAuthFailure func(err error) bool

	// IsRateLimited returns (retryAfter, true) if err is a rate limit; caller may sleep
	// min(retryAfter, MaxRateLimitSleep) before retrying op. If nil, rate limits are not handled.
	IsRateLimited func(err error) (retryAfter time.Duration, ok bool)

	// MaxRateLimitWaits is how many consecutive rate-limit sleeps to perform inside the
	// retry loop. Zero means exhaust immediately (no FS-layer sleeps; ME/workers own
	// RateLimitedUntil waits). Negative means unset → default 10.
	MaxRateLimitWaits int

	// MaxRateLimitSleep caps sleep duration when retryAfter is positive (default 60s).
	MaxRateLimitSleep time.Duration

	// DefaultRateLimitSleep is used when IsRateLimited returns ok with retryAfter <= 0 (default 1s).
	DefaultRateLimitSleep time.Duration

	// MaxIterations bounds total loop iterations (default 64) to prevent runaway retries.
	MaxIterations int

	// OnRateLimitWait is called before sleeping on a rate limit (even when retries eventually succeed).
	OnRateLimitWait func(retryAfter time.Duration, attempt int)

	// OnRateLimitExhausted is called when MaxRateLimitWaits is exceeded.
	OnRateLimitExhausted func(err error)
}

// IsRateLimitedDefault classifies *RateLimitedError and errors.As wrappers.
func IsRateLimitedDefault(err error) (time.Duration, bool) {
	var rl *RateLimitedError
	if errors.As(err, &rl) && rl != nil {
		return rl.RetryAfter, true
	}
	return 0, false
}

// DoWithAuthRetry runs op in a loop: on rate limit it sleeps (capped) and retries;
// on auth failure it runs Refresh at most once then retries op. Returns the last error
// if op never succeeds.
func DoWithAuthRetry(ctx context.Context, cfg RetryConfig, op func() error) error {
	cfg = normalizeRetryConfig(cfg)
	var refreshed bool
	rateWaits := 0
	for i := 0; i < cfg.MaxIterations; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := op()
		if err == nil {
			return nil
		}

		if cfg.IsRateLimited != nil {
			if d, ok := cfg.IsRateLimited(err); ok {
				if rateWaits >= cfg.MaxRateLimitWaits {
					if cfg.OnRateLimitExhausted != nil {
						cfg.OnRateLimitExhausted(err)
					}
					return err
				}
				sleep := d
				if sleep <= 0 {
					sleep = cfg.DefaultRateLimitSleep
				}
				if sleep > cfg.MaxRateLimitSleep {
					sleep = cfg.MaxRateLimitSleep
				}
				if cfg.OnRateLimitWait != nil {
					cfg.OnRateLimitWait(sleep, rateWaits+1)
				}
				if err := sleepCtx(ctx, sleep); err != nil {
					return err
				}
				rateWaits++
				continue
			}
		}
		rateWaits = 0

		if cfg.IsAuthFailure != nil && cfg.IsAuthFailure(err) && cfg.Refresh != nil && !refreshed {
			if rerr := cfg.Refresh(ctx); rerr != nil {
				return fmt.Errorf("%w (refresh failed: %v)", err, rerr)
			}
			refreshed = true
			continue
		}

		return err
	}
	return fmt.Errorf("credentials: DoWithAuthRetry exceeded MaxIterations (%d)", cfg.MaxIterations)
}

func normalizeRetryConfig(cfg RetryConfig) RetryConfig {
	if cfg.MaxRateLimitWaits < 0 {
		cfg.MaxRateLimitWaits = 10
	}
	if cfg.MaxRateLimitSleep <= 0 {
		cfg.MaxRateLimitSleep = 60 * time.Second
	}
	if cfg.DefaultRateLimitSleep <= 0 {
		cfg.DefaultRateLimitSleep = time.Second
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 64
	}
	if cfg.IsAuthFailure == nil {
		cfg.IsAuthFailure = func(err error) bool { return errors.Is(err, ErrNeedsRefresh) }
	}
	return cfg
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

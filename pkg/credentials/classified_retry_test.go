// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package credentials

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestDoWithClassifiedRetryFatalNoRetry(t *testing.T) {
	var calls int
	err := DoWithClassifiedRetry(t.Context(), ClassifiedRetryConfig{
		Classify: func(err error) types.FSErrorClassification {
			return types.FSErrorClassification{Bucket: types.FSErrorFatal}
		},
	}, func() error {
		calls++
		return syscall.EACCES
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1", calls)
	}
}

func TestDoWithClassifiedRetryGenericRetryable(t *testing.T) {
	var calls int
	err := DoWithClassifiedRetry(t.Context(), ClassifiedRetryConfig{
		Classify: func(err error) types.FSErrorClassification {
			return types.FSErrorClassification{Bucket: types.FSErrorRetryable, ErrorCode: "EINTR"}
		},
		GenericRetrySleep: time.Millisecond,
		MaxGenericRetries: 3,
	}, func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d want 3", calls)
	}
}

func TestDoWithClassifiedRetryAmbiguousPromotion(t *testing.T) {
	tr := types.NewAmbiguousErrorTracker(types.AmbiguousTrackerConfig{
		MinSamples: 2, BurstCount: 2, BurstWindow: time.Second, HighWorkerFloor: 2,
	})
	var suspected int
	var calls int
	cfg := ClassifiedRetryConfig{
		Operation: "ListChildren",
		Classify: func(err error) types.FSErrorClassification {
			return types.FSErrorClassification{Bucket: types.FSErrorAmbiguous, ErrorCode: "EIO"}
		},
		AmbiguousTracker: tr,
		WorkerCount:      func() int { return 8 },
		AmbiguousRetrySleep: time.Millisecond,
		MaxAmbiguousRetries: 8,
		RetryConfig: RetryConfig{
			DefaultRateLimitSleep: time.Millisecond,
			MaxRateLimitWaits:     4,
			OnRateLimitWait:       func(time.Duration, int) {},
		},
		OnSuspectedThrottle: func(types.FSErrorClassification, int) { suspected++ },
	}
	// Seed tracker so first call promotes immediately.
	now := time.Now()
	tr.Record("ListChildren", "EIO", 8, now)
	tr.Record("ListChildren", "EIO", 8, now.Add(10*time.Millisecond))

	err := DoWithClassifiedRetry(t.Context(), cfg, func() error {
		calls++
		if calls < 2 {
			return fmt.Errorf("io err: %w", syscall.EIO)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if suspected == 0 {
		t.Fatal("expected suspected throttle callback")
	}
}

func TestDoWithClassifiedRetryExplicitThrottleSeparateFromGeneric(t *testing.T) {
	var rateWaits int
	var calls int
	err := DoWithClassifiedRetry(t.Context(), ClassifiedRetryConfig{
		Classify: func(err error) types.FSErrorClassification {
			if calls == 1 {
				return types.FSErrorClassification{Bucket: types.FSErrorRetryable}
			}
			return types.FSErrorClassification{Bucket: types.FSErrorThrottle, RetryAfter: time.Millisecond}
		},
		GenericRetrySleep: time.Millisecond,
		MaxGenericRetries: 2,
		RetryConfig: RetryConfig{
			OnRateLimitWait: func(time.Duration, int) { rateWaits++ },
			MaxRateLimitWaits: 4,
			DefaultRateLimitSleep: time.Millisecond,
		},
	}, func() error {
		calls++
		if calls <= 2 {
			return errors.New("retry")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if rateWaits != 1 {
		t.Fatalf("rateWaits=%d want 1 (generic retry must not inflate throttle path)", rateWaits)
	}
}

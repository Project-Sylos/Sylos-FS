// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package credentials

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDoWithAuthRetrySuccessFirst(t *testing.T) {
	var calls int
	err := DoWithAuthRetry(t.Context(), RetryConfig{}, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoWithAuthRetryAuthThenRefresh(t *testing.T) {
	var calls, refreshCalls int
	cfg := RetryConfig{
		Refresh: func(ctx context.Context) error {
			refreshCalls++
			return nil
		},
		IsAuthFailure: func(err error) bool { return errors.Is(err, ErrNeedsRefresh) },
	}
	err := DoWithAuthRetry(t.Context(), cfg, func() error {
		calls++
		if calls == 1 {
			return fmt.Errorf("provider: %w", ErrNeedsRefresh)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if refreshCalls != 1 {
		t.Fatalf("refreshCalls = %d, want 1", refreshCalls)
	}
}

func TestDoWithAuthRetryAuthNoRefreshReturns(t *testing.T) {
	cfg := RetryConfig{
		Refresh:       nil,
		IsAuthFailure: func(err error) bool { return errors.Is(err, ErrNeedsRefresh) },
	}
	want := fmt.Errorf("wrap: %w", ErrNeedsRefresh)
	err := DoWithAuthRetry(t.Context(), cfg, func() error {
		return want
	})
	if !errors.Is(err, ErrNeedsRefresh) {
		t.Fatalf("got %v, want ErrNeedsRefresh", err)
	}
}

func TestDoWithAuthRetryRefreshFails(t *testing.T) {
	cfg := RetryConfig{
		Refresh: func(ctx context.Context) error {
			return errors.New("refresh boom")
		},
		IsAuthFailure: func(err error) bool { return errors.Is(err, ErrNeedsRefresh) },
	}
	err := DoWithAuthRetry(t.Context(), cfg, func() error {
		return ErrNeedsRefresh
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrNeedsRefresh) {
		t.Fatalf("expected chain to include ErrNeedsRefresh: %v", err)
	}
}

func TestDoWithAuthRetryRateLimitThenOK(t *testing.T) {
	var calls int
	cfg := RetryConfig{
		IsRateLimited:         IsRateLimitedDefault,
		MaxRateLimitSleep:     50 * time.Millisecond,
		DefaultRateLimitSleep: time.Millisecond,
		MaxIterations:         20,
	}
	err := DoWithAuthRetry(t.Context(), cfg, func() error {
		calls++
		if calls < 3 {
			return RateLimited(2 * time.Millisecond)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestIsRateLimitedDefault(t *testing.T) {
	d, ok := IsRateLimitedDefault(RateLimited(5 * time.Second))
	if !ok || d != 5*time.Second {
		t.Fatalf("got (%v, %v)", d, ok)
	}
}

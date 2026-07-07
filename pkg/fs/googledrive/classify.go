// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"google.golang.org/api/googleapi"
)

func ClassifyGoogleDriveError(err error) types.FSErrorClassification {
	if err == nil {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal}
	}
	if errors.Is(err, credentials.ErrNeedsRefresh) {
		return types.FSErrorClassification{Bucket: types.FSErrorRetryable}
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		switch gerr.Code {
		case http.StatusUnauthorized:
			return types.FSErrorClassification{Bucket: types.FSErrorRetryable}
		case http.StatusTooManyRequests:
			return types.FSErrorClassification{Bucket: types.FSErrorThrottle, RetryAfter: headerRetryAfter(gerr)}
		case http.StatusForbidden:
			if googleAPIThrottleReason(gerr) {
				return types.FSErrorClassification{Bucket: types.FSErrorThrottle, RetryAfter: headerRetryAfter(gerr)}
			}
			return types.FSErrorClassification{Bucket: types.FSErrorFatal}
		case http.StatusNotFound:
			return types.FSErrorClassification{Bucket: types.FSErrorFatal}
		case http.StatusRequestTimeout, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return types.FSErrorClassification{Bucket: types.FSErrorRetryable}
		}
		if strings.Contains(strings.ToLower(gerr.Message), "rate limit") {
			return types.FSErrorClassification{Bucket: types.FSErrorThrottle, RetryAfter: headerRetryAfter(gerr)}
		}
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "temporary") {
		return types.FSErrorClassification{Bucket: types.FSErrorRetryable}
	}
	return types.FSErrorClassification{Bucket: types.FSErrorFatal}
}

func googleAPIThrottleReason(gerr *googleapi.Error) bool {
	if gerr == nil {
		return false
	}
	if gerr.Code == http.StatusTooManyRequests {
		return true
	}
	if gerr.Code != http.StatusForbidden {
		return false
	}
	for _, e := range gerr.Errors {
		switch e.Reason {
		case "userRateLimitExceeded", "rateLimitExceeded", "sharingRateLimitExceeded", "quotaExceeded":
			return true
		}
	}
	return strings.Contains(strings.ToLower(gerr.Message), "rate limit")
}

func headerRetryAfter(gerr *googleapi.Error) time.Duration {
	if gerr == nil || gerr.Header == nil {
		return 0
	}
	v := gerr.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	return 0
}

func (d *DriveFS) throttleBackoff(gerr *googleapi.Error) time.Duration {
	if ra := headerRetryAfter(gerr); ra > 0 {
		return ra + types.ThrottleBackoffJitter
	}
	if d.session.degradation != nil {
		return d.session.degradation.ScheduleThrottleBackoff()
	}
	return time.Second + types.ThrottleBackoffJitter
}

func (d *DriveFS) classifyError(err error) types.FSErrorClassification {
	class := ClassifyGoogleDriveError(err)
	if class.Bucket != types.FSErrorThrottle {
		return class
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		class.RetryAfter = d.throttleBackoff(gerr)
	} else if class.RetryAfter <= 0 {
		class.RetryAfter = d.throttleBackoff(nil)
	}
	return class
}

func (d *DriveFS) withClassifiedRetry(ctx context.Context, operation string, op func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var tracker *types.AmbiguousErrorTracker
	if d.session.degradation != nil {
		tracker = d.session.degradation.AmbiguousTracker()
	}
	return credentials.DoWithClassifiedRetry(ctx, credentials.ClassifiedRetryConfig{
		RetryConfig: credentials.RetryConfig{
			MaxIterations:         8,
			MaxRateLimitWaits:     0,
			MaxRateLimitSleep:     types.MaxThrottleBackoff,
			DefaultRateLimitSleep: time.Second + types.ThrottleBackoffJitter,
			IsAuthFailure: func(err error) bool {
				if errors.Is(err, credentials.ErrNeedsRefresh) {
					return true
				}
				var gerr *googleapi.Error
				return errors.As(err, &gerr) && gerr.Code == http.StatusUnauthorized
			},
			Refresh: func(rctx context.Context) error {
				d.session.tokens.ClearAccessToken(d.session.connectionID)
				return d.session.RefreshAccessToken(rctx)
			},
			OnRateLimitWait: func(retryAfter time.Duration, attempt int) {
				d.recordDegradation(types.FSDegradationRateLimit, operation, retryAfter)
			},
			OnRateLimitExhausted: func(err error) {
				var gerr *googleapi.Error
				retry := time.Second + types.ThrottleBackoffJitter
				if errors.As(err, &gerr) {
					retry = d.throttleBackoff(gerr)
				}
				d.recordDegradation(types.FSDegradationRateLimit, operation, retry)
			},
		},
		Operation:        operation,
		Classify:         d.classifyError,
		AmbiguousTracker: tracker,
		WorkerCount:      d.ActiveWorkers,
		OnSuspectedThrottle: func(class types.FSErrorClassification, attempt int) {
			d.recordDegradation(types.FSDegradationSuspectedRateLimit, operation, 250*time.Millisecond)
		},
	}, func() error {
		err := op()
		if err == nil && d.session.degradation != nil {
			d.session.degradation.ClearThrottleStreak()
		}
		return err
	})
}

func (d *DriveFS) recordDegradation(kind types.FSDegradationKind, operation string, retryAfter time.Duration) {
	if d.session.degradation == nil {
		return
	}
	d.session.degradation.RecordSignal(types.FSDegradationSignal{
		Kind:       kind,
		RetryAfter: retryAfter,
		Operation:  operation,
		At:         time.Now(),
	})
}

// noopReadCloser for empty bodies
type noopReadCloser struct{}

func (noopReadCloser) Read(p []byte) (int, error) { return 0, io.EOF }
func (noopReadCloser) Close() error             { return nil }

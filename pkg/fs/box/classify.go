// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package box

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// ClassifyBoxError maps Box API errors to FS retry buckets.
//
// Policy: honor HTTP 429 + Retry-After as throttle; HTTP 401 as auth refresh;
// everything else is fatal (quota, not found, conflict, permission).
func ClassifyBoxError(err error) types.FSErrorClassification {
	if err == nil {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal}
	}
	if errors.Is(err, credentials.ErrNeedsRefresh) {
		return types.FSErrorClassification{Bucket: types.FSErrorRetryable, ErrorCode: "needs_refresh"}
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		code := strings.ToLower(strings.TrimSpace(apiErr.Code))
		if code == "" {
			code = "box_error"
		}
		switch apiErr.Status {
		case http.StatusUnauthorized:
			return types.FSErrorClassification{Bucket: types.FSErrorRetryable, ErrorCode: code}
		case http.StatusTooManyRequests:
			return types.FSErrorClassification{Bucket: types.FSErrorThrottle, ErrorCode: code}
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return types.FSErrorClassification{Bucket: types.FSErrorRetryable, ErrorCode: code}
		default:
			return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: code}
		}
	}
	return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "unknown"}
}

func (d *BoxFS) throttleBackoff(apiErr *APIError) time.Duration {
	if apiErr != nil && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter + types.ThrottleBackoffJitter
	}
	if d.session.degradation != nil {
		return d.session.degradation.ScheduleThrottleBackoff()
	}
	return time.Second + types.ThrottleBackoffJitter
}

func (d *BoxFS) classifyError(err error) types.FSErrorClassification {
	class := ClassifyBoxError(err)
	if class.Bucket != types.FSErrorThrottle {
		return class
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		class.RetryAfter = d.throttleBackoff(apiErr)
	} else if class.RetryAfter <= 0 {
		class.RetryAfter = d.throttleBackoff(nil)
	}
	return class
}

func (d *BoxFS) withClassifiedRetry(ctx context.Context, operation string, op func() error) error {
	if ctx == nil {
		ctx = context.Background()
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
				var apiErr *APIError
				return errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized
			},
			Refresh: func(rctx context.Context) error {
				d.session.tokens.ClearAccessToken(d.session.connectionID)
				return d.session.RefreshAccessToken(rctx)
			},
			OnRateLimitWait: func(retryAfter time.Duration, attempt int) {
				d.recordDegradation(types.FSDegradationRateLimit, operation, retryAfter)
			},
			OnRateLimitExhausted: func(err error) {
				var apiErr *APIError
				retry := time.Second + types.ThrottleBackoffJitter
				if errors.As(err, &apiErr) {
					retry = d.throttleBackoff(apiErr)
				}
				// Always record so ME AIMD + UI see RateLimitedUntil (even without Retry-After).
				d.recordDegradation(types.FSDegradationRateLimit, operation, retry)
			},
		},
		Operation:           operation,
		Classify:            d.classifyError,
		AmbiguousTracker:    nil,
		WorkerCount:         d.ActiveWorkers,
		OnSuspectedThrottle: nil,
	}, func() error {
		err := op()
		if err == nil && d.session.degradation != nil {
			d.session.degradation.ClearThrottleStreak()
		}
		return err
	})
}

func (d *BoxFS) recordDegradation(kind types.FSDegradationKind, operation string, retryAfter time.Duration) {
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

// RegisterCredentialsPayload builds stored credentials JSON from UI token POST.
func RegisterCredentialsPayload(refreshToken, clientID, clientSecret string, scopes []string) ([]byte, error) {
	stored := cloud.StoredCredentials{
		Provider:     cloud.ProviderBox,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       scopes,
		TokenURI:     boxTokenURL,
	}
	return json.Marshal(stored)
}

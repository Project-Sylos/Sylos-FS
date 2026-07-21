// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// ClassifyDropboxError maps Dropbox API errors to FS retry buckets.
//
// Policy: Dropbox returns explicit HTTP semantics — we only sleep the FS on true rate
// limits (HTTP 429 + Retry-After). Everything else is a known failure; retrying or
// inflating backoff will not fix path, permission, quota, or conflict errors.
//
// Reference: https://developers.dropbox.com/error-handling-guide
//
// Buckets:
//   - FSErrorThrottle — HTTP 429 only
//   - FSErrorRetryable — HTTP 401 (OAuth refresh; not a sleep/backoff case)
//   - FSErrorFatal — all other API and transport errors (tag preserved as ErrorCode for logs)
func ClassifyDropboxError(err error) types.FSErrorClassification {
	if err == nil {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal}
	}
	if errors.Is(err, credentials.ErrNeedsRefresh) {
		return types.FSErrorClassification{Bucket: types.FSErrorRetryable, ErrorCode: "needs_refresh"}
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		tag := dropboxErrorCode(apiErr)
		return classifyDropboxAPIError(apiErr.Status, tag)
	}

	return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "unknown"}
}

func dropboxErrorCode(apiErr *APIError) string {
	tag := strings.ToLower(strings.TrimSpace(apiErr.ErrorTag))
	if tag == "" && apiErr.ErrorSummary != "" {
		tag = strings.ToLower(dropboxErrorTagFromSummary(apiErr.ErrorSummary))
	}
	if tag == "" && len(apiErr.Body) > 0 {
		var body apiErrorBody
		if json.Unmarshal(apiErr.Body, &body) == nil {
			tag = strings.ToLower(extractDropboxErrorTag(body.Error, body.ErrorSummary))
		}
	}
	if tag == "" {
		return "dropbox_error"
	}
	return tag
}

func classifyDropboxAPIError(status int, code string) types.FSErrorClassification {
	switch status {
	case http.StatusUnauthorized:
		return retryable(code, "unauthorized")
	case http.StatusTooManyRequests:
		return throttle(code, "too_many_requests")
	default:
		return fatal(code, code)
	}
}

func fatal(tag, code string) types.FSErrorClassification {
	if code == "" {
		code = tag
	}
	return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: code}
}

func retryable(tag, code string) types.FSErrorClassification {
	if code == "" {
		code = tag
	}
	return types.FSErrorClassification{Bucket: types.FSErrorRetryable, ErrorCode: code}
}

func throttle(tag, code string) types.FSErrorClassification {
	if code == "" {
		code = tag
	}
	return types.FSErrorClassification{Bucket: types.FSErrorThrottle, ErrorCode: code}
}

func apiErrRetryAfter(apiErr *APIError) time.Duration {
	if apiErr == nil {
		return 0
	}
	if apiErr.RetryAfter > 0 {
		return time.Duration(apiErr.RetryAfter * float64(time.Second))
	}
	return 0
}

func (d *DropboxFS) throttleBackoff(apiErr *APIError) time.Duration {
	if ra := apiErrRetryAfter(apiErr); ra > 0 {
		return ra + types.ThrottleBackoffJitter
	}
	if d.session.degradation != nil {
		return d.session.degradation.ScheduleThrottleBackoff()
	}
	return time.Second + types.ThrottleBackoffJitter
}

func (d *DropboxFS) classifyError(err error) types.FSErrorClassification {
	class := ClassifyDropboxError(err)
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

func (d *DropboxFS) withClassifiedRetry(ctx context.Context, operation string, op func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return credentials.DoWithClassifiedRetry(ctx, credentials.ClassifiedRetryConfig{
		RetryConfig: credentials.RetryConfig{
			MaxIterations:         8,
			MaxRateLimitWaits:     0, // exhaust immediately; ME WaitRateLimited owns the window
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

func (d *DropboxFS) recordDegradation(kind types.FSDegradationKind, operation string, retryAfter time.Duration) {
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
		Provider:     cloud.ProviderDropbox,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       scopes,
		TokenURI:     dropboxTokenURL,
	}
	return json.Marshal(stored)
}

// noopReadCloser for empty bodies.
type noopReadCloser struct{}

func (noopReadCloser) Read(p []byte) (int, error) { return 0, io.EOF }
func (noopReadCloser) Close() error             { return nil }

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// ClassifyError maps Graph / transport errors into FS error buckets.
func ClassifyError(err error) types.FSErrorClassification {
	if err == nil {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal}
	}
	if errors.Is(err, credentials.ErrNeedsRefresh) {
		return types.FSErrorClassification{Bucket: types.FSErrorRetryable}
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusUnauthorized:
			return types.FSErrorClassification{Bucket: types.FSErrorRetryable}
		case http.StatusTooManyRequests, http.StatusServiceUnavailable:
			return types.FSErrorClassification{Bucket: types.FSErrorThrottle, RetryAfter: apiErr.RetryAfter}
		case http.StatusNotFound, http.StatusForbidden, http.StatusConflict, http.StatusBadRequest:
			return types.FSErrorClassification{Bucket: types.FSErrorFatal}
		case http.StatusRequestTimeout, http.StatusBadGateway, http.StatusGatewayTimeout:
			return types.FSErrorClassification{Bucket: types.FSErrorRetryable}
		}
		code := strings.ToLower(apiErr.Code)
		if strings.Contains(code, "throttl") || strings.Contains(code, "activitylimit") {
			return types.FSErrorClassification{Bucket: types.FSErrorThrottle, RetryAfter: apiErr.RetryAfter}
		}
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "temporary") || strings.Contains(msg, "connection reset") {
		return types.FSErrorClassification{Bucket: types.FSErrorRetryable}
	}
	return types.FSErrorClassification{Bucket: types.FSErrorFatal}
}

// IsAuthFailure reports whether err should trigger token refresh.
func IsAuthFailure(err error) bool {
	if errors.Is(err, credentials.ErrNeedsRefresh) {
		return true
	}
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized
}

// ThrottleBackoff returns Retry-After or scheduled degradation backoff.
func ThrottleBackoff(err error, degradation *types.FSDegradationState) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter + types.ThrottleBackoffJitter
	}
	if degradation != nil {
		return degradation.ScheduleThrottleBackoff()
	}
	return time.Second + types.ThrottleBackoffJitter
}

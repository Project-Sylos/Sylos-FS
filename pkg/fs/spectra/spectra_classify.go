// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package spectra

import (
	"errors"
	"strings"

	"codeberg.org/Sylos/Spectra/sdk"
	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// ClassifySpectraError maps Spectra SDK / chaos errors into FS retry buckets.
// Explicit 429-style rate limits are throttle-only; they do not share the ambiguous path.
func ClassifySpectraError(err error) types.FSErrorClassification {
	if err == nil {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal}
	}
	if rl, ok := sdk.IsRateLimited(err); ok && rl != nil {
		return types.FSErrorClassification{
			Bucket:     types.FSErrorThrottle,
			ErrorCode:  "429",
			RetryAfter: rl.RetryAfter,
		}
	}
	if _, ok := sdk.IsUnauthorized(err); ok {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "auth"}
	}
	if d, ok := credentials.RateLimitFromError(err); ok {
		return types.FSErrorClassification{
			Bucket:     types.FSErrorThrottle,
			ErrorCode:  "429",
			RetryAfter: d,
		}
	}
	if errors.Is(err, credentials.ErrNeedsRefresh) {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "auth"}
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "not found"):
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "not_found"}
	case strings.Contains(lower, "required"), strings.Contains(lower, "not a folder"), strings.Contains(lower, "invalid"):
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "invalid"}
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "temporary"):
		return types.FSErrorClassification{Bucket: types.FSErrorRetryable, ErrorCode: "transient"}
	default:
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "error"}
	}
}

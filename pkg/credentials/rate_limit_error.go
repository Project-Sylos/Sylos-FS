// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package credentials

import (
	"errors"
	"time"
)

// RateLimitFromError extracts retry-after from a rate-limited error, if any.
func RateLimitFromError(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	var rl *RateLimitedError
	if errors.As(err, &rl) && rl != nil {
		return rl.RetryAfter, true
	}
	return IsRateLimitedDefault(err)
}

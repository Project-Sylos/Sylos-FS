// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package spectra

import (
	"errors"
	"testing"
	"time"

	"codeberg.org/Sylos/Spectra/sdk"
	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestClassifySpectraErrorThrottle(t *testing.T) {
	err := &sdk.RateLimitedError{RetryAfter: 500 * time.Millisecond, Endpoint: "ListChildren"}
	class := ClassifySpectraError(err)
	if class.Bucket != types.FSErrorThrottle || class.ErrorCode != "429" {
		t.Fatalf("got %+v", class)
	}
	if class.RetryAfter != 500*time.Millisecond {
		t.Fatalf("retryAfter=%v", class.RetryAfter)
	}
	if !class.InflateThrottleBackoff() {
		t.Fatal("throttle should inflate backoff")
	}
}

func TestClassifySpectraErrorFatalNotFound(t *testing.T) {
	class := ClassifySpectraError(errors.New("node not found: abc"))
	if class.Bucket != types.FSErrorFatal || class.ErrorCode != "not_found" {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifySpectraErrorAuthFatal(t *testing.T) {
	class := ClassifySpectraError(credentials.ErrNeedsRefresh)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifySpectraErrorRetryableTransient(t *testing.T) {
	class := ClassifySpectraError(errors.New("temporary timeout"))
	if class.Bucket != types.FSErrorRetryable {
		t.Fatalf("got %+v", class)
	}
	if class.InflateThrottleBackoff() {
		t.Fatal("retryable must not inflate throttle")
	}
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"google.golang.org/api/googleapi"
)

func TestClassifyGoogleDriveErrorUnauthorized(t *testing.T) {
	err := &googleapi.Error{Code: http.StatusUnauthorized}
	class := ClassifyGoogleDriveError(err)
	if class.Bucket != types.FSErrorRetryable {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyGoogleDriveErrorThrottle(t *testing.T) {
	err := &googleapi.Error{
		Code: http.StatusForbidden,
		Errors: []googleapi.ErrorItem{{Reason: "userRateLimitExceeded"}},
	}
	class := ClassifyGoogleDriveError(err)
	if class.Bucket != types.FSErrorThrottle {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyGoogleDriveError403NotThrottle(t *testing.T) {
	err := &googleapi.Error{
		Code: http.StatusForbidden,
		Errors: []googleapi.ErrorItem{{Reason: "insufficientFilePermissions"}},
	}
	class := ClassifyGoogleDriveError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyGoogleDriveErrorNotFound(t *testing.T) {
	err := &googleapi.Error{Code: http.StatusNotFound}
	class := ClassifyGoogleDriveError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyGoogleDriveErrorNeedsRefresh(t *testing.T) {
	class := ClassifyGoogleDriveError(credentials.ErrNeedsRefresh)
	if class.Bucket != types.FSErrorRetryable {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyGoogleDriveErrorTransientMessage(t *testing.T) {
	class := ClassifyGoogleDriveError(errors.New("connection timeout"))
	if class.Bucket != types.FSErrorRetryable {
		t.Fatalf("got %+v", class)
	}
}

func TestParseRetryAfterHeader(t *testing.T) {
	err := &googleapi.Error{
		Code: http.StatusTooManyRequests,
		Header: http.Header{
			"Retry-After": []string{"5"},
		},
	}
	class := ClassifyGoogleDriveError(err)
	if class.RetryAfter != 5*time.Second {
		t.Fatalf("retryAfter=%v want 5s", class.RetryAfter)
	}
}

func TestDriveThrottleBackoffExponential(t *testing.T) {
	d := &DriveFS{session: &Session{degradation: types.NewFSDegradationState()}}
	gerr := &googleapi.Error{Code: http.StatusTooManyRequests}
	d1 := d.throttleBackoff(gerr)
	d2 := d.throttleBackoff(gerr)
	if d2 <= d1 {
		t.Fatalf("expected increasing backoff d1=%v d2=%v", d1, d2)
	}
}

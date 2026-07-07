// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestClassifyDropboxErrorUnauthorized(t *testing.T) {
	err := &APIError{Status: http.StatusUnauthorized}
	class := ClassifyDropboxError(err)
	if class.Bucket != types.FSErrorRetryable {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyDropboxErrorThrottle429(t *testing.T) {
	err := &APIError{
		Status:     http.StatusTooManyRequests,
		ErrorTag:   "too_many_requests",
		RetryAfter: 5,
	}
	class := ClassifyDropboxError(err)
	if class.Bucket != types.FSErrorThrottle {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyDropboxError409TooManyWriteOperationsIsFatal(t *testing.T) {
	err := &APIError{
		Status:   http.StatusConflict,
		ErrorTag: "too_many_write_operations",
	}
	class := ClassifyDropboxError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v want fatal", class)
	}
	if class.ErrorCode != "too_many_write_operations" {
		t.Fatalf("ErrorCode=%q", class.ErrorCode)
	}
}

func TestClassifyDropboxErrorPathNotFoundFatal(t *testing.T) {
	err := &APIError{
		Status:       http.StatusConflict,
		ErrorSummary: "path/not_found/...",
		ErrorTag:     "path/not_found",
	}
	class := ClassifyDropboxError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyDropboxErrorPathNotFoundFromNestedJSON(t *testing.T) {
	raw := []byte(`{"error_summary":"path/not_found/","error":{".tag":"path","path":{".tag":"not_found"}}}`)
	var body apiErrorBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	tag := extractDropboxErrorTag(body.Error, body.ErrorSummary)
	if tag != "path/not_found" {
		t.Fatalf("tag=%q want path/not_found", tag)
	}
	err := &APIError{Status: http.StatusConflict, ErrorTag: tag, ErrorSummary: body.ErrorSummary}
	class := ClassifyDropboxError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyDropboxErrorIncorrectOffsetFatal(t *testing.T) {
	err := &APIError{
		Status:   http.StatusConflict,
		ErrorTag: "incorrect_offset",
	}
	class := ClassifyDropboxError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v want fatal", class)
	}
}

func TestClassifyDropboxError403IsFatal(t *testing.T) {
	err := &APIError{Status: http.StatusForbidden, ErrorTag: "insufficient_permissions"}
	class := ClassifyDropboxError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyDropboxError5xxIsFatal(t *testing.T) {
	err := &APIError{Status: http.StatusInternalServerError, ErrorTag: "internal_error"}
	class := ClassifyDropboxError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v want fatal", class)
	}
}

func TestClassifyDropboxErrorNeedsRefresh(t *testing.T) {
	class := ClassifyDropboxError(credentials.ErrNeedsRefresh)
	if class.Bucket != types.FSErrorRetryable {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyDropboxErrorNetworkIsFatal(t *testing.T) {
	class := ClassifyDropboxError(credentials.ErrNeedsRefresh) // keep one retryable
	_ = class
	// Non-API errors are fatal.
	class = ClassifyDropboxError(&APIError{Status: 0})
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyErrorUsesExplicitRetryAfterOnly(t *testing.T) {
	d := &DropboxFS{session: &Session{degradation: types.NewFSDegradationState()}}
	err := &APIError{Status: http.StatusTooManyRequests, ErrorTag: "too_many_requests", RetryAfter: 3}
	class := d.classifyError(err)
	if class.RetryAfter != 3*time.Second+types.ThrottleBackoffJitter {
		t.Fatalf("retryAfter=%v", class.RetryAfter)
	}
}

func TestClassifyErrorNoRetryAfterWithoutHeader(t *testing.T) {
	d := &DropboxFS{session: &Session{degradation: types.NewFSDegradationState()}}
	err := &APIError{Status: http.StatusTooManyRequests, ErrorTag: "too_many_requests"}
	class := d.classifyError(err)
	if class.RetryAfter != 0 {
		t.Fatalf("expected no retryAfter, got %v", class.RetryAfter)
	}
}

func TestRegisterCredentialsPayload(t *testing.T) {
	b, err := RegisterCredentialsPayload("rt", "cid", "secret", []string{"files.metadata.read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty payload")
	}
}

func TestDropboxErrorTagFromSummary(t *testing.T) {
	if got := dropboxErrorTagFromSummary("path/not_found/..."); got != "path/not_found" {
		t.Fatalf("got %q", got)
	}
}

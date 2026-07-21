// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package box

import (
	"net/http"
	"testing"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestClassifyBoxErrorUnauthorized(t *testing.T) {
	err := &APIError{Status: http.StatusUnauthorized, Code: "unauthorized"}
	class := ClassifyBoxError(err)
	if class.Bucket != types.FSErrorRetryable {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyBoxErrorThrottle429(t *testing.T) {
	err := &APIError{
		Status:     http.StatusTooManyRequests,
		Code:       "rate_limit_exceeded",
		RetryAfter: 5 * time.Second,
	}
	class := ClassifyBoxError(err)
	if class.Bucket != types.FSErrorThrottle {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyBoxErrorNotFoundFatal(t *testing.T) {
	err := &APIError{Status: http.StatusNotFound, Code: "not_found"}
	class := ClassifyBoxError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyBoxErrorNeedsRefresh(t *testing.T) {
	class := ClassifyBoxError(credentials.ErrNeedsRefresh)
	if class.Bucket != types.FSErrorRetryable || class.ErrorCode != "needs_refresh" {
		t.Fatalf("got %+v", class)
	}
}

func TestParsePendingFileID(t *testing.T) {
	id := pendingFileID("123", "report.pdf")
	parent, name, ok := parsePendingFileID(id)
	if !ok || parent != "123" || name != "report.pdf" {
		t.Fatalf("parse failed: ok=%v parent=%q name=%q", ok, parent, name)
	}
}

func TestResolveRootFolderID(t *testing.T) {
	if got := resolveRootFolderID(types.Folder{ServiceID: ""}); got != "0" {
		t.Fatalf("empty=%q", got)
	}
	if got := resolveRootFolderID(types.Folder{ServiceID: "root"}); got != "0" {
		t.Fatalf("root=%q", got)
	}
	if got := resolveRootFolderID(types.Folder{ServiceID: "42"}); got != "42" {
		t.Fatalf("id=%q", got)
	}
}

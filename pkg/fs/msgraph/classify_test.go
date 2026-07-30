// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import (
	"net/http"
	"testing"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestClassifyErrorUnauthorized(t *testing.T) {
	err := &APIError{Status: http.StatusUnauthorized}
	class := ClassifyError(err)
	if class.Bucket != types.FSErrorRetryable {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyErrorThrottle(t *testing.T) {
	err := &APIError{Status: http.StatusTooManyRequests, RetryAfter: 5 * time.Second}
	class := ClassifyError(err)
	if class.Bucket != types.FSErrorThrottle || class.RetryAfter != 5*time.Second {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyErrorServiceUnavailable(t *testing.T) {
	err := &APIError{Status: http.StatusServiceUnavailable}
	class := ClassifyError(err)
	if class.Bucket != types.FSErrorThrottle {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyErrorForbidden(t *testing.T) {
	err := &APIError{Status: http.StatusForbidden}
	class := ClassifyError(err)
	if class.Bucket != types.FSErrorFatal {
		t.Fatalf("got %+v", class)
	}
}

func TestClassifyErrorNeedsRefresh(t *testing.T) {
	class := ClassifyError(credentials.ErrNeedsRefresh)
	if class.Bucket != types.FSErrorRetryable {
		t.Fatalf("got %+v", class)
	}
}

func TestItemPath(t *testing.T) {
	if got := ItemPath("", "root"); got != "/me/drive/root" {
		t.Fatalf("got %q", got)
	}
	if got := ItemPath("drv", "abc"); got != "/drives/drv/items/abc" {
		t.Fatalf("got %q", got)
	}
}

func TestPendingFileID(t *testing.T) {
	id := PendingFileID("parent", "name.txt", 42)
	parent, name, size, ok := ParsePendingFileID(id)
	if !ok || parent != "parent" || name != "name.txt" || size != 42 {
		t.Fatalf("parse %q -> %q %q %d %v", id, parent, name, size, ok)
	}
	legacy := "pending:parent:legacy.txt"
	parent, name, size, ok = ParsePendingFileID(legacy)
	if !ok || parent != "parent" || name != "legacy.txt" || size != -1 {
		t.Fatalf("legacy parse %q -> %q %q %d %v", legacy, parent, name, size, ok)
	}
}

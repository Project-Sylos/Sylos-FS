// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build unix

package types

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestClassifyLocalErrorBuckets(t *testing.T) {
	cases := []struct {
		err    error
		bucket FSErrorBucket
	}{
		{syscall.EACCES, FSErrorFatal},
		{syscall.ENOENT, FSErrorFatal},
		{syscall.ENOTDIR, FSErrorFatal},
		{syscall.ENOSPC, FSErrorFatal},
		{syscall.EROFS, FSErrorFatal},
		{syscall.EINVAL, FSErrorFatal},
		{syscall.EIO, FSErrorAmbiguous},
		{syscall.EAGAIN, FSErrorAmbiguous},
		{syscall.EBUSY, FSErrorAmbiguous},
		{syscall.EMFILE, FSErrorAmbiguous},
		{syscall.ETIMEDOUT, FSErrorRetryable},
		{syscall.EINTR, FSErrorRetryable},
	}
	for _, tc := range cases {
		got := ClassifyLocalError(fmt.Errorf("wrap: %w", tc.err))
		if got.Bucket != tc.bucket {
			t.Fatalf("%v -> %s want %s", tc.err, got.Bucket, tc.bucket)
		}
	}
}

// Only load-correlated errnos may reach the tracker, since Ambiguous is the sole bucket
// that behavioral promotion can turn into FS_THROTTLE.
func TestClassifyLocalErrorUnknownIsNotPromotable(t *testing.T) {
	unknown := []error{
		errors.New("some opaque failure"),
		&os.PathError{Op: "readdirent", Path: "/x", Err: errors.New("weird")},
		os.ErrClosed,
	}
	for _, err := range unknown {
		got := ClassifyLocalError(err)
		if got.Bucket == FSErrorAmbiguous {
			t.Fatalf("%v classified Ambiguous; unknown errors must not be promotable", err)
		}
		if !got.Retryable() {
			t.Fatalf("%v should still be retryable, got %s", err, got.Bucket)
		}
	}
}

// Tracker keys must stay low-cardinality or burst/sample counters never accumulate.
func TestClassifyLocalErrorUnknownCodeIsStable(t *testing.T) {
	a := ClassifyLocalError(errors.New("failure one"))
	b := ClassifyLocalError(errors.New("failure two"))
	if a.ErrorCode != b.ErrorCode {
		t.Fatalf("unknown codes differ: %q vs %q", a.ErrorCode, b.ErrorCode)
	}
	if a.ErrorCode == "" {
		t.Fatal("unknown code should be a stable non-empty token")
	}
}

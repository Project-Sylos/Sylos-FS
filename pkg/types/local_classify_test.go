// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build unix

package types

import (
	"fmt"
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
		{syscall.EIO, FSErrorAmbiguous},
		{syscall.EAGAIN, FSErrorAmbiguous},
		{syscall.ETIMEDOUT, FSErrorRetryable},
	}
	for _, tc := range cases {
		got := ClassifyLocalError(fmt.Errorf("wrap: %w", tc.err))
		if got.Bucket != tc.bucket {
			t.Fatalf("%v -> %s want %s", tc.err, got.Bucket, tc.bucket)
		}
	}
}

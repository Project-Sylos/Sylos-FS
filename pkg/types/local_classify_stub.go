// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !unix

package types

import (
	"errors"
	"os"
)

// ClassifyLocalError maps os errors from local-path FS calls into buckets (non-Unix stub).
// Unrecognized errors are Retryable rather than Ambiguous: without platform errno mapping
// there is no evidence of load shedding, and Ambiguous is the only bucket that can be
// promoted to throttle (which scales workers down).
//
// Windows / winfsp sharing- and lock-violation mapping still needs a platform-specific
// classifier before those signals can drive the autoscaler.
func ClassifyLocalError(err error) FSErrorClassification {
	if err == nil {
		return FSErrorClassification{Bucket: FSErrorFatal}
	}
	switch {
	case errors.Is(err, ErrPathBlocked):
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: "path_blocked"}
	case os.IsNotExist(err):
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: "not_exist"}
	case os.IsPermission(err):
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: "permission"}
	case errors.Is(err, os.ErrInvalid):
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: "invalid"}
	default:
		return FSErrorClassification{Bucket: FSErrorRetryable, ErrorCode: "unclassified"}
	}
}

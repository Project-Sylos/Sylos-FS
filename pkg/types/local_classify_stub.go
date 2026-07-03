// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !unix

package types

import (
	"errors"
	"os"
)

// ClassifyLocalError maps os errors from local-path FS calls into buckets (non-Unix stub).
func ClassifyLocalError(err error) FSErrorClassification {
	if err == nil {
		return FSErrorClassification{Bucket: FSErrorFatal}
	}
	code := "unknown"
	switch {
	case os.IsNotExist(err):
		code = "not_exist"
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: code}
	case os.IsPermission(err):
		code = "permission"
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: code}
	default:
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && pathErr != nil {
			return FSErrorClassification{Bucket: FSErrorAmbiguous, ErrorCode: pathErr.Err.Error()}
		}
		return FSErrorClassification{Bucket: FSErrorAmbiguous, ErrorCode: code}
	}
}

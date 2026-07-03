// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build unix

package types

import (
	"errors"
	"os"
	"syscall"
)

// ClassifyLocalError maps POSIX / os errors from local-path FS calls into buckets.
// Declared FS type is not used — sync/FUSE mounts still surface local errno values.
func ClassifyLocalError(err error) FSErrorClassification {
	if err == nil {
		return FSErrorClassification{Bucket: FSErrorFatal}
	}
	code := localErrorCode(err)
	switch {
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: code}
	case errors.Is(err, syscall.ENOENT), os.IsNotExist(err):
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: code}
	case os.IsPermission(err):
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: code}
	case errors.Is(err, syscall.EIO):
		return FSErrorClassification{Bucket: FSErrorAmbiguous, ErrorCode: code}
	case errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EBUSY):
		return FSErrorClassification{Bucket: FSErrorAmbiguous, ErrorCode: code}
	case errors.Is(err, syscall.ETIMEDOUT):
		return FSErrorClassification{Bucket: FSErrorRetryable, ErrorCode: code}
	case errors.Is(err, syscall.EINTR):
		return FSErrorClassification{Bucket: FSErrorRetryable, ErrorCode: code}
	default:
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && pathErr != nil {
			return FSErrorClassification{Bucket: FSErrorAmbiguous, ErrorCode: localErrorCode(pathErr.Err)}
		}
		return FSErrorClassification{Bucket: FSErrorAmbiguous, ErrorCode: code}
	}
}

func localErrorCode(err error) string {
	if err == nil {
		return "nil"
	}
	switch {
	case errors.Is(err, syscall.EACCES):
		return "EACCES"
	case errors.Is(err, syscall.EPERM):
		return "EPERM"
	case errors.Is(err, syscall.ENOENT):
		return "ENOENT"
	case errors.Is(err, syscall.EIO):
		return "EIO"
	case errors.Is(err, syscall.EAGAIN):
		return "EAGAIN"
	case errors.Is(err, syscall.EBUSY):
		return "EBUSY"
	case errors.Is(err, syscall.ETIMEDOUT):
		return "ETIMEDOUT"
	case errors.Is(err, syscall.EINTR):
		return "EINTR"
	case os.IsNotExist(err):
		return "not_exist"
	case os.IsPermission(err):
		return "permission"
	default:
		return err.Error()
	}
}

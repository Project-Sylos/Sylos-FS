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
//
// Only errnos that correlate with load or contention are Ambiguous, because that is the
// only bucket the behavioral tracker can promote to throttle (and therefore the only one
// that can scale workers down). Unrecognized errors are Retryable: they still get a bounded
// retry, but they never look like a rate limit.
func ClassifyLocalError(err error) FSErrorClassification {
	if err == nil {
		return FSErrorClassification{Bucket: FSErrorFatal}
	}
	if errors.Is(err, ErrPathBlocked) {
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: "path_blocked"}
	}
	code := localErrorCode(err)
	switch {
	case isLocalFatalErrno(err):
		return FSErrorClassification{Bucket: FSErrorFatal, ErrorCode: code}
	case isLocalLoadErrno(err):
		return FSErrorClassification{Bucket: FSErrorAmbiguous, ErrorCode: code}
	case errors.Is(err, syscall.ETIMEDOUT), errors.Is(err, syscall.EINTR):
		return FSErrorClassification{Bucket: FSErrorRetryable, ErrorCode: code}
	default:
		return FSErrorClassification{Bucket: FSErrorRetryable, ErrorCode: code}
	}
}

// isLocalLoadErrno reports errnos whose rate scales with concurrency, so a worker
// step-down is a plausible remedy. Network drives, FUSE and winfsp layers surface
// these when the backing store (or the API behind it) sheds load.
func isLocalLoadErrno(err error) bool {
	switch {
	case errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EBUSY):
		return true
	case errors.Is(err, syscall.EIO):
		return true
	case errors.Is(err, syscall.EMFILE), errors.Is(err, syscall.ENFILE):
		return true
	case errors.Is(err, syscall.ENOMEM):
		return true
	default:
		return false
	}
}

// isLocalFatalErrno reports errors that retrying cannot fix.
func isLocalFatalErrno(err error) bool {
	switch {
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return true
	case errors.Is(err, syscall.ENOENT), os.IsNotExist(err):
		return true
	case os.IsPermission(err):
		return true
	case errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.EISDIR):
		return true
	case errors.Is(err, syscall.ENAMETOOLONG), errors.Is(err, syscall.ELOOP):
		return true
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT), errors.Is(err, syscall.EFBIG):
		return true
	case errors.Is(err, syscall.EROFS), errors.Is(err, syscall.EXDEV):
		return true
	case errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.ENOTSUP):
		return true
	default:
		return false
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
	case errors.Is(err, syscall.EMFILE):
		return "EMFILE"
	case errors.Is(err, syscall.ENFILE):
		return "ENFILE"
	case errors.Is(err, syscall.ENOMEM):
		return "ENOMEM"
	case errors.Is(err, syscall.ENOTDIR):
		return "ENOTDIR"
	case errors.Is(err, syscall.EISDIR):
		return "EISDIR"
	case errors.Is(err, syscall.ENAMETOOLONG):
		return "ENAMETOOLONG"
	case errors.Is(err, syscall.ELOOP):
		return "ELOOP"
	case errors.Is(err, syscall.ENOSPC):
		return "ENOSPC"
	case errors.Is(err, syscall.EDQUOT):
		return "EDQUOT"
	case errors.Is(err, syscall.EFBIG):
		return "EFBIG"
	case errors.Is(err, syscall.EROFS):
		return "EROFS"
	case errors.Is(err, syscall.EXDEV):
		return "EXDEV"
	case errors.Is(err, syscall.EINVAL):
		return "EINVAL"
	case errors.Is(err, syscall.ENOTSUP):
		return "ENOTSUP"
	case errors.Is(err, syscall.ETIMEDOUT):
		return "ETIMEDOUT"
	case errors.Is(err, syscall.EINTR):
		return "EINTR"
	case os.IsNotExist(err):
		return "not_exist"
	case os.IsPermission(err):
		return "permission"
	default:
		// Keep tracker keys low-cardinality; raw messages would fragment the counters.
		return "unclassified"
	}
}

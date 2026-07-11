// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sftp

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func classifySftpError(err error) types.FSErrorClassification {
	if err == nil {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal}
	}
	if errors.Is(err, io.EOF) {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "eof"}
	}
	if errors.Is(err, os.ErrNotExist) {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "not_found"}
	}
	if errors.Is(err, os.ErrPermission) {
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "permission_denied"}
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "broken pipe"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "temporary failure"),
		strings.Contains(msg, "i/o timeout"):
		return types.FSErrorClassification{Bucket: types.FSErrorAmbiguous, ErrorCode: "network"}
	case strings.Contains(msg, "permission denied"):
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "permission_denied"}
	case strings.Contains(msg, "no such file"):
		return types.FSErrorClassification{Bucket: types.FSErrorFatal, ErrorCode: "not_found"}
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE, syscall.ETIMEDOUT:
			return types.FSErrorClassification{Bucket: types.FSErrorAmbiguous, ErrorCode: errno.Error()}
		}
	}
	return types.FSErrorClassification{Bucket: types.FSErrorRetryable, ErrorCode: "unknown"}
}

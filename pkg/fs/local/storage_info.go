// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"context"
	"path/filepath"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// GetStorageInfo reports free/total capacity for path (or the adapter root when path is empty).
func (l *LocalFS) GetStorageInfo(ctx context.Context, path string) (types.StorageInfo, error) {
	_ = ctx
	target := path
	if target == "" {
		target = l.root
	}
	if target == "" {
		target = "/"
	}
	// Prefer an absolute path so Windows drive letters resolve correctly.
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	return FilesystemUsage(target)
}

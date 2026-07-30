// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sftp

import (
	"context"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// GetStorageInfo is unavailable for SFTP (no portable free-space API).
func (f *SftpFS) GetStorageInfo(ctx context.Context, path string) (types.StorageInfo, error) {
	_ = ctx
	_ = path
	return types.UnavailableStorage(), nil
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package onedrive

import (
	"context"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// GetStorageInfo reports OneDrive quota via Microsoft Graph.
func (d *OneDriveFS) GetStorageInfo(ctx context.Context, path string) (types.StorageInfo, error) {
	return d.ops.GetStorageInfo(ctx, path)
}

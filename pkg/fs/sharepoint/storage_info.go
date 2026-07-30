// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sharepoint

import (
	"context"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// GetStorageInfo reports SharePoint document-library quota via Microsoft Graph.
func (d *SharePointFS) GetStorageInfo(ctx context.Context, path string) (types.StorageInfo, error) {
	return d.ops.GetStorageInfo(ctx, path)
}

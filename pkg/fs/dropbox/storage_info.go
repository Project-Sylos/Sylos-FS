// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// GetStorageInfo reports Dropbox account quota via users/get_space_usage.
func (d *DropboxFS) GetStorageInfo(ctx context.Context, path string) (types.StorageInfo, error) {
	_ = path
	if ctx == nil {
		ctx = context.Background()
	}
	var info types.StorageInfo
	err := d.withClassifiedRetry(ctx, "GetStorageInfo", func() error {
		// Account quota is not namespace-scoped; call without Path-Root.
		client, err := d.session.apiClient(ctx, "")
		if err != nil {
			return err
		}
		used, allocated, err := client.getSpaceUsage(ctx)
		if err != nil {
			return err
		}
		info = types.StorageInfo{
			Available:  true,
			UsedBytes:  used,
			TotalBytes: allocated,
			Source:     "dropbox.space_usage",
		}
		if allocated > 0 && allocated >= used {
			info.FreeBytes = allocated - used
		} else if allocated <= 0 {
			info.Unlimited = true
			info.FreeBytes = 0
		}
		return nil
	})
	if err != nil {
		return types.UnavailableStorage(), err
	}
	return info, nil
}

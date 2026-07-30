// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"context"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// GetStorageInfo reports Google Drive quota via about.get storageQuota.
func (d *DriveFS) GetStorageInfo(ctx context.Context, path string) (types.StorageInfo, error) {
	_ = path
	if ctx == nil {
		ctx = context.Background()
	}
	var info types.StorageInfo
	err := d.withClassifiedRetry(ctx, "GetStorageInfo", func() error {
		srv, err := d.session.driveService(ctx)
		if err != nil {
			return err
		}
		about, err := srv.About.Get().Fields("storageQuota").Do()
		if err != nil {
			return err
		}
		info = types.StorageInfo{Available: true, Source: "drive.about"}
		if about.StorageQuota == nil {
			info.Unlimited = true
			return nil
		}
		q := about.StorageQuota
		info.UsedBytes = q.Usage
		if q.Limit > 0 {
			info.TotalBytes = q.Limit
			if q.Limit >= q.Usage {
				info.FreeBytes = q.Limit - q.Usage
			}
		} else {
			info.Unlimited = true
		}
		return nil
	})
	if err != nil {
		return types.UnavailableStorage(), err
	}
	return info, nil
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import (
	"context"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// GetStorageInfo reports Graph drive quota for the adapter's effective drive.
func (a *AdapterOps) GetStorageInfo(ctx context.Context, path string) (types.StorageInfo, error) {
	_ = path
	if ctx == nil {
		ctx = context.Background()
	}
	var info types.StorageInfo
	err := a.WithClassifiedRetry(ctx, "GetStorageInfo", func() error {
		client, err := a.Client(ctx)
		if err != nil {
			return err
		}
		drive, err := client.GetDriveQuota(ctx, DrivePath(a.EffectiveDriveID()))
		if err != nil {
			return err
		}
		info = types.StorageInfo{Available: true, Source: "graph.quota"}
		if drive.Quota == nil {
			info.Unlimited = true
			return nil
		}
		q := drive.Quota
		info.TotalBytes = q.Total
		info.UsedBytes = q.Used
		info.FreeBytes = q.Remaining
		if q.Total <= 0 && q.Remaining <= 0 {
			info.Unlimited = true
		}
		return nil
	})
	if err != nil {
		return types.UnavailableStorage(), err
	}
	return info, nil
}

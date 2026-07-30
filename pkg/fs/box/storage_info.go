// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package box

import (
	"context"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// GetStorageInfo reports Box account quota from GET /users/me.
func (d *BoxFS) GetStorageInfo(ctx context.Context, path string) (types.StorageInfo, error) {
	_ = path
	if ctx == nil {
		ctx = context.Background()
	}
	var info types.StorageInfo
	err := d.withClassifiedRetry(ctx, "GetStorageInfo", func() error {
		client, err := d.session.apiClient(ctx)
		if err != nil {
			return err
		}
		user, err := client.GetCurrentUser(ctx)
		if err != nil {
			return err
		}
		info = types.StorageInfo{
			Available:  true,
			TotalBytes: user.SpaceAmount,
			UsedBytes:  user.SpaceUsed,
			Source:     "box.users_me",
		}
		if user.SpaceAmount <= 0 {
			info.Unlimited = true
		} else if user.SpaceAmount >= user.SpaceUsed {
			info.FreeBytes = user.SpaceAmount - user.SpaceUsed
		}
		return nil
	})
	if err != nil {
		return types.UnavailableStorage(), err
	}
	return info, nil
}

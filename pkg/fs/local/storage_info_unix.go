// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build unix

package local

import (
	"fmt"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"golang.org/x/sys/unix"
)

// FilesystemUsage returns capacity for the volume containing path via statfs.
func FilesystemUsage(path string) (types.StorageInfo, error) {
	if path == "" {
		path = "/"
	}
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return types.UnavailableStorage(), fmt.Errorf("statfs %s: %w", path, err)
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		return types.UnavailableStorage(), fmt.Errorf("statfs %s: invalid block size", path)
	}
	total := int64(st.Blocks) * bsize
	free := int64(st.Bavail) * bsize
	info := types.StorageInfo{
		Available:  true,
		TotalBytes: total,
		FreeBytes:  free,
		Source:     "statfs",
	}
	if total >= free {
		info.UsedBytes = total - free
	}
	return info, nil
}

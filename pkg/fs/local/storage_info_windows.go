// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build windows

package local

import (
	"fmt"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"golang.org/x/sys/windows"
)

// FilesystemUsage returns capacity for the volume containing path via GetDiskFreeSpaceEx.
func FilesystemUsage(path string) (types.StorageInfo, error) {
	if path == "" {
		path = `C:\`
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return types.UnavailableStorage(), err
	}
	var available, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &available, &totalBytes, &totalFree); err != nil {
		return types.UnavailableStorage(), fmt.Errorf("GetDiskFreeSpaceEx %s: %w", path, err)
	}
	info := types.StorageInfo{
		Available:  true,
		TotalBytes: int64(totalBytes),
		FreeBytes:  int64(available),
		Source:     "GetDiskFreeSpaceEx",
	}
	if totalBytes >= available {
		info.UsedBytes = int64(totalBytes - available)
	}
	return info, nil
}

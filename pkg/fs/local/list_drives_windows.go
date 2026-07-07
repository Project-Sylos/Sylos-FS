// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build windows

package local

import (
	"fmt"
	"os"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"golang.org/x/sys/windows"
)

// ListDrives enumerates accessible Windows drive letters with capacity metadata.
func ListDrives() ([]types.DriveInfo, error) {
	var drives []types.DriveInfo
	for letter := 'A'; letter <= 'Z'; letter++ {
		drivePath := string(letter) + ":\\"
		info, err := os.Stat(drivePath)
		if err != nil || !info.IsDir() {
			continue
		}

		driveType := "unknown"
		if _, err := os.ReadDir(drivePath); err == nil {
			driveType = "fixed"
		}

		drive := types.DriveInfo{
			Path:        drivePath,
			MountPoint:  drivePath,
			Device:      drivePath,
			Mounted:     true,
			DisplayName: fmt.Sprintf("%s:", string(letter)),
			Type:        driveType,
		}
		applyFilesystemUsage(&drive, drivePath)
		drives = append(drives, drive)
	}
	return drives, nil
}

func applyFilesystemUsage(d *types.DriveInfo, path string) {
	if d == nil {
		return
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	var available, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &available, &totalBytes, &totalFree); err != nil {
		return
	}
	d.TotalBytes = int64(totalBytes)
	d.FreeBytes = int64(available)
	if totalBytes >= available {
		d.UsedBytes = int64(totalBytes - available)
	}
}

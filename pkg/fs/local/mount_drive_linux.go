// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build linux

package local

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// MountDrive mounts a block device using udisks2 (udisksctl).
func MountDrive(devicePath string) (types.DriveInfo, error) {
	devicePath = filepath.Clean(devicePath)
	if !strings.HasPrefix(devicePath, "/dev/") {
		return types.DriveInfo{}, fmt.Errorf("not a block device path: %s", devicePath)
	}

	resolved, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return types.DriveInfo{}, fmt.Errorf("device not found: %w", err)
	}
	devicePath = resolved

	if drive, ok := findMountedDrive(devicePath); ok {
		return drive, nil
	}

	if _, err := exec.LookPath("udisksctl"); err != nil {
		return types.DriveInfo{}, fmt.Errorf("udisksctl is not available; install udisks2 to mount drives from Sylos")
	}

	cmd := exec.Command("udisksctl", "mount", "-b", devicePath, "--no-user-interaction")
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if output != "" {
			return types.DriveInfo{}, fmt.Errorf("%s", output)
		}
		return types.DriveInfo{}, fmt.Errorf("mount failed: %w", err)
	}

	mountPoint := parseUdisksMountOutput(output)
	if mountPoint != "" {
		if drive, ok := findDriveByMountPoint(mountPoint); ok {
			return drive, nil
		}
	}

	if drive, ok := findMountedDrive(devicePath); ok {
		return drive, nil
	}

	return types.DriveInfo{}, fmt.Errorf("drive mounted but could not resolve mount point from: %s", output)
}

func parseUdisksMountOutput(output string) string {
	const marker = " at "
	idx := strings.LastIndex(output, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(output[idx+len(marker):])
}

func findMountedDrive(devicePath string) (types.DriveInfo, bool) {
	drives, err := ListDrives()
	if err != nil {
		return types.DriveInfo{}, false
	}

	resolvedDevice, _ := filepath.EvalSymlinks(devicePath)
	for _, drive := range drives {
		if !drive.Mounted {
			continue
		}
		if drive.Device == devicePath || drive.Path == devicePath {
			return drive, true
		}
		if resolvedDevice != "" && drive.Device != "" {
			resolvedDriveDevice, err := filepath.EvalSymlinks(drive.Device)
			if err == nil && resolvedDriveDevice == resolvedDevice {
				return drive, true
			}
		}
	}
	return types.DriveInfo{}, false
}

func findDriveByMountPoint(mountPoint string) (types.DriveInfo, bool) {
	drives, err := ListDrives()
	if err != nil {
		return types.DriveInfo{}, false
	}
	for _, drive := range drives {
		if drive.Mounted && (drive.Path == mountPoint || drive.MountPoint == mountPoint) {
			return drive, true
		}
	}
	return types.DriveInfo{}, false
}

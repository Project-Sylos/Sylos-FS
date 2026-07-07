// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !linux

package local

import (
	"fmt"
	"runtime"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// MountDrive mounts a block device on supported platforms.
func MountDrive(devicePath string) (types.DriveInfo, error) {
	return types.DriveInfo{}, fmt.Errorf("mounting drives is not supported on %s", runtime.GOOS)
}

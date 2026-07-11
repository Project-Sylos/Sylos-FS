// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build darwin

package local

import (
	"strings"

	"golang.org/x/sys/unix"
)

func darwinVolumeType(path string) string {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return "fixed"
	}
	fstype := strings.ToLower(unixByteString(st.Fstypename[:]))
	from := unixByteString(st.Mntfromname[:])
	if _, ok := darwinNetworkFSTypes[fstype]; ok || darwinIsNetworkSource(from) {
		return "network"
	}
	return "fixed"
}

func unixByteString(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}

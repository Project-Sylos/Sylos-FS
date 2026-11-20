// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build darwin

package local

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// kIsAlias is the FinderInfo flag bit for alias files (Carbon/Finder).
const kIsAlias = 0x8000

// isMacOSAliasFile reports Finder aliases via com.apple.FinderInfo (type only;
// does not resolve the alias target).
func isMacOSAliasFile(path string) bool {
	var buf [32]byte
	n, err := unix.Getxattr(path, "com.apple.FinderInfo", buf[:])
	if err != nil || n < 10 {
		return false
	}
	if string(buf[0:4]) == "alis" {
		return true
	}
	flags := binary.BigEndian.Uint16(buf[8:10])
	return flags&kIsAlias != 0
}

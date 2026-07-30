// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build unix

package local

// blockedPathPrefixes are absolute path prefixes that must never be traversed or
// opened during a migration. Pseudo and device filesystems park or explode under
// concurrent listing (/proc/pid/fd, /sys, etc.).
var blockedPathPrefixes = []string{
	"/proc",
	"/sys",
	"/dev",
	"/run",
}

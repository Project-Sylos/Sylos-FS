// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build windows

package local

// blockedPathPrefixes are path prefixes (forward-slash normalized, lowercased
// matching applied in isBlockedPath) that must never be traversed or opened.
var blockedPathPrefixes = []string{
	`//./`,          // \\.\ device namespace
	`//?/globalroot`, // \\?\GLOBALROOT
	`/proc`,         // WSL / uncommon unix-style mounts under Windows paths
	`/sys`,
	`/dev`,
}

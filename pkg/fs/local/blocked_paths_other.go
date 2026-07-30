// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !unix && !windows

package local

// blockedPathPrefixes for non-unix, non-windows platforms (WASM, etc.).
var blockedPathPrefixes = []string{
	"/proc",
	"/sys",
	"/dev",
	"/run",
}

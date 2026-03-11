// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !(unix && !windows)

package local

import "os"

// Stub when syscall.Stat_t is unavailable (Windows, WASM, etc.).
func deviceID(fi os.FileInfo) (uint64, bool) {
	_ = fi
	return 0, false
}

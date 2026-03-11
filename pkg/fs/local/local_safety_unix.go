// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build unix && !windows

package local

import (
	"os"
	"syscall"
)

func deviceID(fi os.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, false
	}
	return uint64(st.Dev), true
}

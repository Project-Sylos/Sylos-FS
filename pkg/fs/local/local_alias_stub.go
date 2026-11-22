// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !darwin

package local

func isMacOSAliasFile(path string) bool {
	_ = path
	return false
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !windows && !darwin

package local

func darwinVolumeType(_ string) string {
	return "fixed"
}

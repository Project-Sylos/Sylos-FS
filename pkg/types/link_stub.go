// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

// IsSkippedLinkStubName reports Windows Shell Link / Internet Shortcut basenames.
// These are ordinary files that only point elsewhere; V1 omits them (does not follow).
func IsSkippedLinkStubName(name string) bool {
	switch FileExtension(name) {
	case "lnk", "url":
		return true
	default:
		return false
	}
}

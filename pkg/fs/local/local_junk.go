// Copyright 2026 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"strings"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// isSilentJunkBasename omits desktop metadata files even if somehow a directory.
func isSilentJunkBasename(name string) bool {
	return strings.EqualFold(name, ".DS_Store") || strings.EqualFold(name, "Thumbs.db")
}

// omitListedFile reports whether a regular file should be left out of ListChildren.
func omitListedFile(name, fullPath string) bool {
	if isSilentJunkBasename(name) || types.IsSkippedLinkStubName(name) {
		return true
	}
	return isMacOSAliasFile(fullPath)
}

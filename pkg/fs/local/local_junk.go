// Copyright 2026 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import "strings"

func isSilentJunkBasename(name string) bool {
	return strings.EqualFold(name, ".DS_Store") || strings.EqualFold(name, "Thumbs.db")
}

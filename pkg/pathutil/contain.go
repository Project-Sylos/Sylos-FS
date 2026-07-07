// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package pathutil provides cross-platform path containment checks.
package pathutil

import (
	"path/filepath"
	"strings"
)

// WithinRoot reports whether target is root itself or a descendant of root.
// Both paths are resolved with filepath.Abs and filepath.Clean before comparison.
func WithinRoot(root, target string) (bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	absRoot = filepath.Clean(absRoot)

	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}
	absTarget = filepath.Clean(absTarget)

	if absRoot == absTarget {
		return true, nil
	}

	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		// e.g. different Windows drive letters or invalid UNC pairing
		return false, nil
	}
	if rel == ".." {
		return false, nil
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

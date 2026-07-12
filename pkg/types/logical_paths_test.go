// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types_test

import (
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestListChildrenBasePathPrefersParentPath(t *testing.T) {
	got := types.ListChildrenBasePath("/migration-root", "/Interview Prep")
	if got != "/Interview Prep" {
		t.Fatalf("got %q", got)
	}
}

func TestListChildrenBasePathFallsBackToRoot(t *testing.T) {
	got := types.ListChildrenBasePath("/migration-root", "")
	if got != "/migration-root" {
		t.Fatalf("got %q", got)
	}
}

func TestLogicalParentFromCreateMetadataLocationPath(t *testing.T) {
	meta := map[string]string{"location_path": "/Interview Prep/Beyond Feeback"}
	got := types.LogicalParentFromCreateMetadata(meta, "/")
	if got != "/Interview Prep" {
		t.Fatalf("got %q", got)
	}
}

func TestChildLocationFromCreateMetadata(t *testing.T) {
	meta := map[string]string{"location_path": "/Interview Prep/Beyond Feeback"}
	got := types.ChildLocationFromCreateMetadata(meta, "/", "ignored")
	if got != "/Interview Prep/Beyond Feeback" {
		t.Fatalf("got %q", got)
	}
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package onedrive

import "testing"

func TestForbiddenMigrationRootIDs(t *testing.T) {
	ids := (factory{}).ForbiddenMigrationRootIDs()
	if len(ids) != 1 || ids[0] != "sharedWithMe" {
		t.Fatalf("ForbiddenMigrationRootIDs=%v want [sharedWithMe]", ids)
	}
}

func TestProviderID(t *testing.T) {
	if (factory{}).ProviderID() != "onedrive" {
		t.Fatal("unexpected provider id")
	}
}

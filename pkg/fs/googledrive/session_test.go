// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import "testing"

func TestForbiddenMigrationRootIDs(t *testing.T) {
	ids := (factory{}).ForbiddenMigrationRootIDs()
	if len(ids) != 1 || ids[0] != "sharedWithMe" {
		t.Fatalf("forbidden roots = %v, want [sharedWithMe]", ids)
	}
}

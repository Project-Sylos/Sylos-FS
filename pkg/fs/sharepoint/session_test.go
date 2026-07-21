// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sharepoint

import (
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestProviderID(t *testing.T) {
	if (factory{}).ProviderID() != cloud.ProviderSharePoint {
		t.Fatal("unexpected provider id")
	}
}

func TestForbiddenMigrationRootIDsEmpty(t *testing.T) {
	ids := (factory{}).ForbiddenMigrationRootIDs()
	if len(ids) != 0 {
		t.Fatalf("expected no static forbidden ids, got %v", ids)
	}
}

func TestParseDriveContextSite(t *testing.T) {
	ctx := parseDriveContext(types.Folder{
		ServiceID: "site-1",
		Type:      cloud.RootTypeSharePointSite,
	})
	if ctx.SiteID != "site-1" || ctx.RootType != cloud.RootTypeSharePointSite {
		t.Fatalf("got %+v", ctx)
	}
}

func TestParseDriveContextDrive(t *testing.T) {
	ctx := parseDriveContext(types.Folder{
		ServiceID: "root",
		ParentId:  "drive-1",
		Type:      cloud.RootTypeSharePointDrive,
	})
	if ctx.DriveID != "drive-1" || ctx.FolderID != "root" {
		t.Fatalf("got %+v", ctx)
	}
}

func TestValidateSharePointSiteForbidden(t *testing.T) {
	err := cloud.ValidateMigrationRootFolder(cloud.ProviderSharePoint, types.Folder{
		ServiceID: "site-abc",
		Type:      cloud.RootTypeSharePointSite,
	})
	if err == nil {
		t.Fatal("expected forbidden site root")
	}
}

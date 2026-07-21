// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import (
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestBrowseFolderSharedWithMe(t *testing.T) {
	f, err := BrowseFolder("sharedWithMe", RootTypeSharedWithMe, "")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != RootTypeSharedWithMe || f.ServiceID != "sharedWithMe" {
		t.Fatalf("got %+v", f)
	}
}

func TestBrowseFolderSharedWithMeRequiresRootType(t *testing.T) {
	f, err := BrowseFolder("sharedWithMe", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != types.NodeTypeFolder {
		t.Fatalf("expected plain folder without rootType, got type=%q", f.Type)
	}
}

func TestBrowseFolderSharedDrive(t *testing.T) {
	f, err := BrowseFolder("0ABC123", RootTypeSharedDrive, "")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != RootTypeSharedDrive {
		t.Fatalf("type=%q", f.Type)
	}
	if f.ParentId != "0ABC123" || f.ServiceID != "root" {
		t.Fatalf("got %+v", f)
	}
}

func TestBrowseFolderSharedDriveRequiresDriveID(t *testing.T) {
	_, err := BrowseFolder("", RootTypeSharedDrive, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBrowseFolderRegularFolder(t *testing.T) {
	f, err := BrowseFolder("folder-id-xyz", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != types.NodeTypeFolder || f.ServiceID != "folder-id-xyz" {
		t.Fatalf("got %+v", f)
	}
}

func TestBrowseFolderUnknownRootType(t *testing.T) {
	_, err := BrowseFolder("root", "bogus", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBrowseRootDropboxUser(t *testing.T) {
	f, err := BrowseRoot(Root{ID: "root", DriveID: "ns-home", DisplayName: "My Dropbox", RootType: RootTypeUserRoot})
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != RootTypeUserRoot || f.ServiceID != "root" || f.ParentId != "ns-home" {
		t.Fatalf("got %+v", f)
	}
}

func TestBrowseRootDropboxUserLegacyNamespaceID(t *testing.T) {
	f, err := BrowseFolder("ns-home", RootTypeUserRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if f.ServiceID != "root" || f.ParentId != "ns-home" {
		t.Fatalf("got %+v", f)
	}
}

func TestBrowseRootTeamFolder(t *testing.T) {
	f, err := BrowseRoot(Root{
		ID:          "tf-123",
		DisplayName: "Marketing",
		RootType:    RootTypeTeamFolder,
		DriveID:     "ns-team",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != RootTypeTeamFolder || f.ParentId != "ns-team" || f.ServiceID != "tf-123" {
		t.Fatalf("got %+v", f)
	}
}

func TestBrowseRootSharedFolder(t *testing.T) {
	f, err := BrowseRoot(Root{
		ID:          "sf-456",
		DisplayName: "Shared",
		RootType:    RootTypeSharedFolder,
		DriveID:     "/shared/path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != RootTypeSharedFolder || f.ParentId != "/shared/path" {
		t.Fatalf("got %+v", f)
	}
}

func TestBrowseRootSharedFolderNamespaceFallback(t *testing.T) {
	f, err := BrowseRoot(Root{
		ID:       "sf-789",
		RootType: RootTypeSharedFolder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.ParentId != "sf-789" || f.ServiceID != "sf-789" {
		t.Fatalf("expected shared folder id as namespace ParentId, got %+v", f)
	}
}

func TestBrowseRootTeamSpace(t *testing.T) {
	f, err := BrowseRoot(Root{
		ID:       "teamSpace",
		DriveID:  "ns-root",
		RootType: RootTypeTeamSpace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.ServiceID != "teamSpace" || f.ParentId != "ns-root" {
		t.Fatalf("got %+v", f)
	}
}

func TestIsVirtualRootListing(t *testing.T) {
	if !IsVirtualRootListing("teamSpace", RootTypeTeamSpace) {
		t.Fatal("teamSpace should be virtual")
	}
	if IsVirtualRootListing("id:abc", RootTypeTeamSpace) {
		t.Fatal("nested team space child should not be virtual")
	}
	if !IsVirtualRootListing("sf-1", RootTypeSharedFolder) {
		t.Fatal("shared folder root should be virtual when rootType set")
	}
	if IsVirtualRootListing("id:abc", "") {
		t.Fatal("nested without rootType is not virtual")
	}
	if !IsVirtualRootListing("site-1", RootTypeSharePointSite) {
		t.Fatal("sharepoint site should be virtual")
	}
	if !IsVirtualRootListing("drive-1", RootTypeSharePointDrive) {
		t.Fatal("sharepoint drive should be virtual when rootType set")
	}
	if !IsVirtualRootListing("0", RootTypeMyDrive) {
		t.Fatal("box All Files (id 0) should be virtual")
	}
}

func TestBrowseFolderSharePointSite(t *testing.T) {
	f, err := BrowseFolder("site-abc", RootTypeSharePointSite, "")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != RootTypeSharePointSite || f.ServiceID != "site-abc" {
		t.Fatalf("got %+v", f)
	}
}

func TestBrowseFolderSharePointDrive(t *testing.T) {
	f, err := BrowseFolder("drive-1", RootTypeSharePointDrive, "drive-1")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != RootTypeSharePointDrive || f.ServiceID != "root" || f.ParentId != "drive-1" {
		t.Fatalf("got %+v", f)
	}
}

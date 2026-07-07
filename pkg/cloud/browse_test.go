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

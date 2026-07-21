// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"strings"
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestListPathVirtualRootUsesEmptyDropboxPath(t *testing.T) {
	d := &DropboxFS{
		ctx:  dropboxContext{RootType: cloud.RootTypeUserRoot, NamespaceID: "ns-home"},
		root: types.Folder{ServiceID: "root", LocationPath: "/"},
	}
	if got := d.listPath("ns-home"); got != "" {
		t.Fatalf("listPath(ns-home)=%q want empty root path", got)
	}
	if got := d.listPath("root"); got != "" {
		t.Fatalf("listPath(root)=%q want empty root path", got)
	}
	if got := d.listPath("abc123"); got != "id:abc123" {
		t.Fatalf("listPath(folder)=%q", got)
	}
}

func TestListPathSharedFolderRootUsesPath(t *testing.T) {
	d := &DropboxFS{
		ctx: dropboxContext{
			RootType: cloud.RootTypeSharedFolder,
			RootPath: "/shared/docs",
		},
		root: types.Folder{ServiceID: "sf-1", LocationPath: "/"},
	}
	if got := d.listPath("root"); got != "/shared/docs" {
		t.Fatalf("listPath=%q want /shared/docs", got)
	}
}

func TestSharedRootPathPrefersRootPathOverFolderRef(t *testing.T) {
	d := &DropboxFS{
		ctx: dropboxContext{
			RootType:  cloud.RootTypeUserRoot,
			RootPath:  "/extra/folder",
			FolderRef: "id:should-not-win",
		},
	}
	if got := d.sharedRootPath(); got != "/extra/folder" {
		t.Fatalf("sharedRootPath=%q", got)
	}
}

func TestDropboxPathRef(t *testing.T) {
	if got := dropboxPathRef("abc"); got != "id:abc" {
		t.Fatalf("got %q", got)
	}
	if got := dropboxPathRef("/foo"); got != "/foo" {
		t.Fatalf("got %q", got)
	}
	if strings.TrimSpace(dropboxPathRef("")) != "" {
		t.Fatal("expected empty")
	}
}

func TestParentPathForCreateRoot(t *testing.T) {
	if got := parentPathForCreate("root", "Interview Prep"); got != "/Interview Prep" {
		t.Fatalf("got %q want /Interview Prep", got)
	}
	if got := parentPathForCreate("", "foo"); got != "/foo" {
		t.Fatalf("got %q want /foo", got)
	}
}

func TestNormalizeParentForCreate(t *testing.T) {
	d := &DropboxFS{
		ctx:  dropboxContext{RootType: cloud.RootTypeUserRoot, NamespaceID: "ns-home"},
		root: types.Folder{ServiceID: "root", LocationPath: "/"},
	}
	if got := d.normalizeParentForCreate("root"); got != "" {
		t.Fatalf("normalize(root)=%q want empty", got)
	}
	if got := d.normalizeParentForCreate("ns-home"); got != "" {
		t.Fatalf("normalize(ns-home)=%q want empty", got)
	}
	if got := d.normalizeParentForCreate("id:abc"); got != "id:abc" {
		t.Fatalf("normalize(id:abc)=%q", got)
	}
}

func TestPathRootForAPIUserRootOmitsNamespaceHeader(t *testing.T) {
	d := &DropboxFS{
		ctx: dropboxContext{RootType: cloud.RootTypeUserRoot, NamespaceID: "ns-home"},
	}
	if got := d.pathRootForAPI(); got != "" {
		t.Fatalf("pathRootForAPI()=%q want empty for user_root", got)
	}
}

func TestPathRootForAPISharedFolderNamespace(t *testing.T) {
	d := &DropboxFS{
		ctx: dropboxContext{RootType: cloud.RootTypeSharedFolder, NamespaceID: "sf-ns"},
	}
	if got := d.pathRootForAPI(); got != "sf-ns" {
		t.Fatalf("pathRootForAPI()=%q want sf-ns", got)
	}
}

func TestListPathSharedFolderNamespaceRoot(t *testing.T) {
	d := &DropboxFS{
		ctx: dropboxContext{
			RootType:    cloud.RootTypeSharedFolder,
			NamespaceID: "sf-ns",
		},
		root: types.Folder{ServiceID: "sf-ns", LocationPath: "/"},
	}
	if got := d.listPath("sf-ns"); got != "" {
		t.Fatalf("listPath(sf-ns)=%q want empty", got)
	}
}

func TestMetaToFolderSharedFolderUsesNamespaceID(t *testing.T) {
	d := &DropboxFS{}
	f := d.metaToFolder(fileMetadata{
		Tag:            "folder",
		ID:             "id:fileabc",
		Name:           "Team Docs",
		SharedFolderID: "99",
	}, "/")
	if f.ServiceID != "99" || f.ParentId != "99" || f.Type != types.NodeTypeFolder {
		t.Fatalf("got %+v", f)
	}
}

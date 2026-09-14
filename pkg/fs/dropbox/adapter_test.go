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

func TestCreateAPIPathUsesParentID(t *testing.T) {
	d := &DropboxFS{
		ctx:  dropboxContext{RootType: cloud.RootTypeUserRoot},
		root: types.Folder{ServiceID: "root", Type: cloud.RootTypeUserRoot, LocationPath: "/"},
	}
	got, err := d.createAPIPath("root", "Interview Prep", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/Interview Prep" {
		t.Fatalf("got %q want /Interview Prep", got)
	}
	got, err = d.createAPIPath("", "foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/foo" {
		t.Fatalf("got %q want /foo", got)
	}
	got, err = d.createAPIPath("id:abc", "child", map[string]string{
		"location_path": "/minted-uuid/child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "id:abc/child" {
		t.Fatalf("got %q want id:abc/child (metadata ignored)", got)
	}
}

func TestCreateAPIPathNestedMigrationRoot(t *testing.T) {
	d := &DropboxFS{
		ctx: dropboxContext{
			RootType: cloud.RootTypeUserRoot,
			RootPath: "/extra/folder",
		},
		root: types.Folder{ServiceID: "id:rootfolder", Type: types.NodeTypeFolder, LocationPath: "/"},
	}
	got, err := d.createAPIPath("id:rootfolder", "top", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "id:rootfolder/top" {
		t.Fatalf("got %q want id:rootfolder/top", got)
	}
	got, err = d.createAPIPath("id:parent", "child", map[string]string{"location_path": "/2/child"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "id:parent/child" {
		t.Fatalf("got %q", got)
	}
}

func TestCreateUnderNamespaceRoot(t *testing.T) {
	d := &DropboxFS{
		ctx:  dropboxContext{RootType: cloud.RootTypeUserRoot, NamespaceID: "ns-home"},
		root: types.Folder{ServiceID: "root", Type: cloud.RootTypeUserRoot, LocationPath: "/"},
	}
	if !d.createUnderNamespaceRoot("root") {
		t.Fatal("expected root")
	}
	if !d.createUnderNamespaceRoot("ns-home") {
		t.Fatal("expected ns-home")
	}
	if d.createUnderNamespaceRoot("id:abc") {
		t.Fatal("id:abc is not namespace root")
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

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestParsePendingFileIDLegacyFormat(t *testing.T) {
	parent, name, ok := parsePendingFileID("pending:abc123:my:file:name")
	if !ok || parent != "abc123" || name != "my:file:name" {
		t.Fatalf("parent=%q name=%q ok=%v", parent, name, ok)
	}
}

func TestParsePendingFileIDRejectsLocationPrefix(t *testing.T) {
	if _, _, ok := parsePendingFileID(pendingFileByLocation("/a/b.pdf")); ok {
		t.Fatal("pending-loc ids must not parse as pending:parent:name")
	}
}

func TestNewDropboxWriterParsesLegacyPendingFile(t *testing.T) {
	d := &DropboxFS{}
	w, err := newDropboxWriter(d, t.Context(), "pending:abc123:chunk.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !w.create {
		t.Fatal("expected create upload")
	}
	if w.parentID != "abc123" || w.fileName != "chunk.bin" {
		t.Fatalf("parent=%q name=%q", w.parentID, w.fileName)
	}
}

func TestPendingFileByLocation(t *testing.T) {
	id := pendingFileByLocation("id:parent/report.pdf")
	loc, ok := parsePendingLocationPath(id)
	if !ok || loc != "id:parent/report.pdf" {
		t.Fatalf("loc=%q ok=%v", loc, ok)
	}
	d := &DropboxFS{}
	w, err := newDropboxWriter(d, t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !w.create || w.locationPath != "id:parent/report.pdf" {
		t.Fatalf("writer=%+v", w)
	}
}

func TestResolveUploadPathUsesWirePendingLoc(t *testing.T) {
	d := &DropboxFS{
		ctx:  dropboxContext{RootType: cloud.RootTypeUserRoot, RootPath: "/extra/folder"},
		root: types.Folder{ServiceID: "id:rootfolder", Type: types.NodeTypeFolder},
	}
	w, err := newDropboxWriter(d, t.Context(), pendingFileByLocation("id:parent/child.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.resolveUploadPath(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "id:parent/child.txt" {
		t.Fatalf("got %q", got)
	}
}

func TestCreateAPIPathIgnoresLocationMetadata(t *testing.T) {
	d := &DropboxFS{
		ctx:  dropboxContext{RootType: cloud.RootTypeUserRoot, RootPath: "/extra/folder"},
		root: types.Folder{ServiceID: "id:rootfolder", Type: types.NodeTypeFolder, LocationPath: "/"},
	}
	got, err := d.createAPIPath("id:parent", "child", map[string]string{
		"location_path": "/2/child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "id:parent/child" {
		t.Fatalf("got %q", got)
	}
	// Virtual "root" parent always means namespace/account root, even if adapter.root is a nested folder.
	got, err = d.createAPIPath("root", "top", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/top" {
		t.Fatalf("root create=%q", got)
	}
	got, err = d.createAPIPath("id:rootfolder", "top", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "id:rootfolder/top" {
		t.Fatalf("nested root create=%q", got)
	}
	got, err = d.createAPIPath("id:only", "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "id:only/x" {
		t.Fatalf("got %q", got)
	}
}

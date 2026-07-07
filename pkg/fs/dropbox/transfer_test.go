// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
)

func TestPendingFileIDRootParent(t *testing.T) {
	id := pendingFileID("", "report.pdf")
	parent, name, ok := parsePendingFileID(id)
	if !ok {
		t.Fatalf("parse failed for %q", id)
	}
	if parent != pendingRootParent || name != "report.pdf" {
		t.Fatalf("parent=%q name=%q", parent, name)
	}
}

func TestPendingFileIDRootAlias(t *testing.T) {
	id := pendingFileID("root", "report.pdf")
	parent, name, ok := parsePendingFileID(id)
	if !ok {
		t.Fatalf("parse failed for %q", id)
	}
	if parent != pendingRootParent || name != "report.pdf" {
		t.Fatalf("parent=%q name=%q", parent, name)
	}
}

func TestNewDropboxWriterParsesRootPendingFile(t *testing.T) {
	d := &DropboxFS{}
	w, err := newDropboxWriter(d, t.Context(), pendingFileID("", "chunk.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !w.create {
		t.Fatal("expected create upload")
	}
	if w.parentID != pendingRootParent || w.fileName != "chunk.bin" {
		t.Fatalf("parent=%q name=%q", w.parentID, w.fileName)
	}
}

func TestPendingFileIDNestedParent(t *testing.T) {
	id := pendingFileID("abc123", "my:file:name")
	parent, name, ok := parsePendingFileID(id)
	if !ok {
		t.Fatalf("parse failed for %q", id)
	}
	if parent != "abc123" || name != "my:file:name" {
		t.Fatalf("parent=%q name=%q", parent, name)
	}
}

func TestPendingFileByLocation(t *testing.T) {
	id := pendingFileByLocation("/Interview Prep/report.pdf")
	loc, ok := parsePendingLocationPath(id)
	if !ok || loc != "/Interview Prep/report.pdf" {
		t.Fatalf("loc=%q ok=%v", loc, ok)
	}
	d := &DropboxFS{}
	w, err := newDropboxWriter(d, t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !w.create || w.locationPath != "/Interview Prep/report.pdf" {
		t.Fatalf("writer=%+v", w)
	}
}

func TestResolveLocationPathSharedRoot(t *testing.T) {
	d := &DropboxFS{
		ctx: dropboxContext{
			RootType: cloud.RootTypeSharedFolder,
			RootPath: "/shared/docs",
		},
	}
	got := d.resolveLocationPath("/notes/file.txt")
	if got != "/shared/docs/notes/file.txt" {
		t.Fatalf("got %q", got)
	}
}

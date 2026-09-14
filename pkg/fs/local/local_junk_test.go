// Copyright 2026 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestIsSilentJunkBasename(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: ".DS_Store", want: true},
		{name: ".ds_store", want: true},
		{name: "Thumbs.db", want: true},
		{name: "thumbs.db", want: true},
		{name: "THUMBS.DB", want: true},
		{name: ".DS_Store.bak", want: false},
		{name: "prefix.Thumbs.db", want: false},
		{name: "desktop.ini", want: false},
		{name: "file.tmp", want: false},
		{name: "~$document.docx", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSilentJunkBasename(tt.name); got != tt.want {
				t.Fatalf("isSilentJunkBasename(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestListChildrenOmitsOnlySilentJunkBasenames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		".DS_Store",
		"thumbs.db",
		"desktop.ini",
		"file.tmp",
		"~$document.docx",
		".DS_Store.bak",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "THUMBS.DB"), 0700); err != nil {
		t.Fatal(err)
	}

	localFS, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := localFS.ListChildren(context.Background(), dir, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Folders) != 0 {
		t.Fatalf("unexpected folders: %+v", result.Folders)
	}
	got := make([]string, 0, len(result.Files))
	for _, file := range result.Files {
		got = append(got, file.DisplayName)
	}
	want := []string{"desktop.ini", "file.tmp", "~$document.docx", ".DS_Store.bak"}
	for _, name := range want {
		if !slices.Contains(got, name) {
			t.Errorf("expected %q in listed files, got %v", name, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("listed files = %v, want exactly %v", got, want)
	}
}

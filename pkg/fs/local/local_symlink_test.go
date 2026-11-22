// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build unix && !windows

package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestListChildren_skipsSymlinksByType(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(regPath, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(regPath, filepath.Join(dir, "file-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sub, filepath.Join(dir, "dir-link")); err != nil {
		t.Fatal(err)
	}

	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := l.ListChildren(context.Background(), dir, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Folders) != 1 {
		t.Fatalf("expected 1 folder, got %d (dir symlink must not be listed)", len(result.Folders))
	}
	if result.Folders[0].DisplayName != "subdir" {
		t.Errorf("expected subdir, got %q", result.Folders[0].DisplayName)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 regular file, got %d (file symlink must not be listed)", len(result.Files))
	}
	if result.Files[0].DisplayName != "regular.txt" {
		t.Errorf("expected regular.txt, got %q", result.Files[0].DisplayName)
	}
}

func TestOpenRead_symlink_returnsErrNotRegularFile(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(regPath, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "file-link")
	if err := os.Symlink(regPath, linkPath); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.OpenRead(context.Background(), linkPath)
	if err == nil {
		t.Fatal("expected error opening symlink")
	}
	if !errors.Is(err, ErrNotRegularFile) {
		t.Fatalf("expected ErrNotRegularFile, got %v", err)
	}
}

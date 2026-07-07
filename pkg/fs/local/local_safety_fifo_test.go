// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build unix && !windows

package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestListChildren_skipsFIFO(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(regPath, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifoPath, 0600); err != nil {
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
	if len(result.Folders) != 0 {
		t.Errorf("expected no folders, got %d", len(result.Folders))
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 regular file, got %d (FIFO must not be listed as file)", len(result.Files))
	}
	if result.Files[0].DisplayName != "regular.txt" {
		t.Errorf("expected regular.txt, got %q", result.Files[0].DisplayName)
	}
}

func TestOpenRead_FIFO_returnsErrNotRegularFile(t *testing.T) {
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifoPath, 0600); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.OpenRead(context.Background(), fifoPath)
	if err == nil {
		t.Fatal("expected error opening FIFO")
	}
	if !errors.Is(err, ErrNotRegularFile) {
		t.Fatalf("expected ErrNotRegularFile, got %v", err)
	}
}

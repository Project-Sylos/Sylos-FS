// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build linux || freebsd || netbsd || aix

package local

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRead_PageCacheHints_closeNoPanic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.PageCacheHints = true
	rc, err := l.OpenRead(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(rc)
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
}

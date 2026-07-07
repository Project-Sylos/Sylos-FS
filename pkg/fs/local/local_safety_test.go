// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FIFO/skip tests live in local_safety_fifo_test.go (unix only).

func TestOpenRead_regularFile_ok(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := l.OpenRead(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	buf := make([]byte, 1)
	n, err := rc.Read(buf)
	if err != nil || n != 1 || buf[0] != 'x' {
		t.Fatalf("read failed: n=%d err=%v", n, err)
	}
}

func TestListChildren_notDirectory_returnsError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.ListChildren(context.Background(), p, nil, "")
	if err == nil {
		t.Fatal("expected error listing file path")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected not a directory error, got %v", err)
	}
}

func TestIsPseudoFSPath(t *testing.T) {
	if !isPseudoFSPath("/proc") {
		t.Error("/proc should match")
	}
	if !isPseudoFSPath("/proc/self") {
		t.Error("/proc/self should match")
	}
	if !isPseudoFSPath("/sys/fs") {
		t.Error("/sys/fs should match")
	}
	if isPseudoFSPath("/home/user") {
		t.Error("/home/user should not match")
	}
}

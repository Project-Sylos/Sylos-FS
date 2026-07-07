// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestCreateAndDeleteFolder(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}

	folder, err := l.CreateFolder(context.Background(), dir, "child")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(folder.ServiceID); err != nil {
		t.Fatalf("folder not created: %v", err)
	}

	if err := l.DeleteNode(context.Background(), folder.ServiceID, types.NodeTypeFolder); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(folder.ServiceID); !os.IsNotExist(err) {
		t.Fatalf("expected folder removed, got %v", err)
	}
}

func TestDeleteFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.DeleteNode(context.Background(), filePath, types.NodeTypeFile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, got %v", err)
	}
}

func TestDeleteNodeByAbsolutePathUnderFilesystemRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix filesystem root adapter")
	}

	target := filepath.Join(os.TempDir(), "sylos-delete-root-test")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(target) })

	l, err := NewLocalFS("/")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.DeleteNode(context.Background(), target, types.NodeTypeFolder); err != nil {
		t.Fatalf("delete by absolute path id: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected folder removed, got %v", err)
	}
}

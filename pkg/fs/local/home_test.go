// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestHomeFolder(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	} else {
		t.Setenv("HOME", dir)
	}

	folder, err := HomeFolder()
	if err != nil {
		t.Fatalf("HomeFolder: %v", err)
	}

	wantID := strings.ReplaceAll(filepath.Clean(dir), "\\", "/")
	if folder.ServiceID != wantID {
		t.Fatalf("ServiceID: got %q want %q", folder.ServiceID, wantID)
	}
	if folder.DisplayName != filepath.Base(dir) {
		t.Fatalf("DisplayName: got %q want %q", folder.DisplayName, filepath.Base(dir))
	}
	if folder.Type != "folder" {
		t.Fatalf("Type: got %q want folder", folder.Type)
	}

	wantLoc, wantParent := homeLogicalPaths(filepath.Clean(dir), wantID)
	if folder.LocationPath != wantLoc {
		t.Fatalf("LocationPath: got %q want %q", folder.LocationPath, wantLoc)
	}
	if folder.ParentPath != wantParent {
		t.Fatalf("ParentPath: got %q want %q", folder.ParentPath, wantParent)
	}
	if types.NormalizeLocationPath(folder.LocationPath) != folder.LocationPath {
		t.Fatalf("LocationPath not normalized: %q", folder.LocationPath)
	}
	if runtime.GOOS != "windows" && folder.ParentPath != path.Dir(wantID) {
		t.Fatalf("ParentPath: got %q want %q", folder.ParentPath, path.Dir(wantID))
	}

	if _, err := os.Stat(folder.ServiceID); err != nil {
		t.Fatalf("ServiceID not listable: %v", err)
	}
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// HomeFolder returns the process user's home directory as a browse Folder.
// ServiceID is the absolute OS path (forward slashes); LocationPath is the
// volume-root-relative logical path used by local ListChildren breadcrumbs.
func HomeFolder() (types.Folder, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return types.Folder{}, fmt.Errorf("resolve user home directory: %w", err)
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return types.Folder{}, fmt.Errorf("resolve user home directory: %w", err)
	}
	abs = filepath.Clean(abs)

	fi, err := os.Stat(abs)
	if err != nil {
		return types.Folder{}, fmt.Errorf("stat user home directory: %w", err)
	}
	if !fi.IsDir() {
		return types.Folder{}, fmt.Errorf("user home is not a directory: %s", abs)
	}

	serviceID := strings.ReplaceAll(abs, "\\", "/")
	displayName := filepath.Base(abs)
	if displayName == "" || displayName == "." || displayName == string(filepath.Separator) {
		displayName = serviceID
	}

	parentAbs := filepath.Dir(abs)
	parentID := strings.ReplaceAll(parentAbs, "\\", "/")

	locationPath, parentPath := homeLogicalPaths(abs, serviceID)

	return types.Folder{
		ServiceID:    serviceID,
		ParentId:     parentID,
		ParentPath:   parentPath,
		DisplayName:  displayName,
		LocationPath: locationPath,
		LastUpdated:  fi.ModTime().Format(time.RFC3339),
		Type:         types.NodeTypeFolder,
	}, nil
}

func homeLogicalPaths(abs, serviceID string) (locationPath, parentPath string) {
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(abs)
		volSlash := strings.ReplaceAll(vol, "\\", "/")
		rel := strings.TrimPrefix(serviceID, volSlash)
		locationPath = types.NormalizeLocationPath(rel)
	} else {
		locationPath = types.NormalizeLocationPath(serviceID)
	}
	if locationPath == "/" {
		return "/", "/"
	}
	parentPath = path.Dir(locationPath)
	if parentPath == "" {
		parentPath = "/"
	}
	return locationPath, parentPath
}

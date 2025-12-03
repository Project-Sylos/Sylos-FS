// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package fs

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Project-Sylos/Sylos-FS/pkg/types"
)

// LocalFS implements FSAdapter for the local filesystem.
type LocalFS struct {
	root string // absolute, normalized root path for this migration
}

// NewLocalFS constructs a new LocalFS adapter rooted at the given path.
func NewLocalFS(rootPath string) (*LocalFS, error) {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	// Normalize to forward slashes
	abs = strings.ReplaceAll(filepath.Clean(abs), "\\", "/")
	return &LocalFS{root: abs}, nil
}

// relativize builds a relative path from the parent's path and the node name.
func (l *LocalFS) relativize(nodeName string, parentRelPath string) string {
	// If parent is root, child path is /{childName}
	if parentRelPath == "/" {
		return "/" + nodeName
	}
	// Otherwise, build path as {parentRelPath}/{name}
	return parentRelPath + "/" + nodeName
}

// ListChildren lists immediate children of the given node identifier (absolute path).
func (l *LocalFS) ListChildren(identifier string) (types.ListResult, error) {
	var result types.ListResult

	// Get parent's relative path by stripping root
	normalizedParentId := strings.ReplaceAll(identifier, "\\", "/")
	root := strings.TrimSuffix(l.root, "/")
	p := strings.ReplaceAll(filepath.Clean(normalizedParentId), "\\", "/")
	var parentRelPath string
	if p == root || p == root+"/" {
		parentRelPath = "/"
	} else if strings.HasPrefix(p, root) {
		rel := strings.TrimPrefix(p[len(root):], "/")
		if rel == "" {
			parentRelPath = "/"
		} else {
			parentRelPath = "/" + rel
		}
	} else {
		parentRelPath = "/"
	}

	entries, err := os.ReadDir(identifier)
	if err != nil {
		return result, err
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fullPath := filepath.Join(identifier, entry.Name())
		fullPath = strings.ReplaceAll(fullPath, "\\", "/")

		// Use parent's relative path to build child's relative path
		rel := l.relativize(entry.Name(), parentRelPath)

		if entry.IsDir() {
			result.Folders = append(result.Folders, types.Folder{
				Id:           fullPath,      // physical identifier
				ParentId:     identifier,    // parent physical path
				ParentPath:   parentRelPath, // parent's relative path
				DisplayName:  entry.Name(),
				LocationPath: rel, // logical, root-relative path
				LastUpdated:  info.ModTime().Format(time.RFC3339),
				Type:         types.NodeTypeFolder,
			})
		} else {
			result.Files = append(result.Files, types.File{
				Id:           fullPath,
				ParentId:     identifier,
				ParentPath:   parentRelPath, // parent's relative path
				DisplayName:  entry.Name(),
				LocationPath: rel,
				LastUpdated:  info.ModTime().Format(time.RFC3339),
				Size:         info.Size(),
				Type:         types.NodeTypeFile,
			})
		}
	}

	return result, nil
}

// DownloadFile opens the absolute file path (identifier) for streaming.
func (l *LocalFS) DownloadFile(identifier string) (io.ReadCloser, error) {
	return os.Open(identifier)
}

// CreateFolder creates a new folder under a parent absolute path.
func (l *LocalFS) CreateFolder(parentId, name string) (types.Folder, error) {
	fullPath := filepath.Join(parentId, name)
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")

	// Get parent's relative path by stripping root
	normalizedParentId := strings.ReplaceAll(parentId, "\\", "/")
	root := strings.TrimSuffix(l.root, "/")
	p := strings.ReplaceAll(filepath.Clean(normalizedParentId), "\\", "/")
	var parentRelPath string
	if p == root || p == root+"/" {
		parentRelPath = "/"
	} else if strings.HasPrefix(p, root) {
		rel := strings.TrimPrefix(p[len(root):], "/")
		if rel == "" {
			parentRelPath = "/"
		} else {
			parentRelPath = "/" + rel
		}
	} else {
		parentRelPath = "/"
	}

	if err := os.MkdirAll(fullPath, os.ModePerm); err != nil {
		return types.Folder{}, err
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return types.Folder{}, err
	}

	// Use parent's relative path to build child's relative path
	relPath := l.relativize(name, parentRelPath)

	return types.Folder{
		Id:           fullPath,
		ParentId:     parentId,
		ParentPath:   parentRelPath,
		DisplayName:  name,
		LocationPath: relPath,
		LastUpdated:  info.ModTime().Format(time.RFC3339),
		Type:         types.NodeTypeFolder,
	}, nil
}

// UploadFile writes a new file at dest identifier.
func (l *LocalFS) UploadFile(destId string, content io.Reader) (types.File, error) {
	// Normalize destination path
	normalizedDestId := strings.ReplaceAll(destId, "\\", "/")

	// Get parent directory and normalize it
	parentDir := filepath.Dir(normalizedDestId)
	parentDir = strings.ReplaceAll(parentDir, "\\", "/")

	// Get parent's relative path by stripping root
	root := strings.TrimSuffix(l.root, "/")
	p := strings.ReplaceAll(filepath.Clean(parentDir), "\\", "/")
	var parentRelPath string
	if p == root || p == root+"/" {
		parentRelPath = "/"
	} else if strings.HasPrefix(p, root) {
		rel := strings.TrimPrefix(p[len(root):], "/")
		if rel == "" {
			parentRelPath = "/"
		} else {
			parentRelPath = "/" + rel
		}
	} else {
		parentRelPath = "/"
	}

	nodeName := filepath.Base(normalizedDestId)
	f, err := os.Create(destId)
	if err != nil {
		return types.File{}, err
	}
	defer f.Close()

	n, err := io.Copy(f, content)
	if err != nil {
		return types.File{}, err
	}

	info, _ := os.Stat(destId)

	// Use parent's relative path to build file's relative path
	relPath := l.relativize(nodeName, parentRelPath)

	return types.File{
		Id:           destId,
		ParentId:     parentDir, // Note: this is the absolute parent path, not destId's parent
		ParentPath:   parentRelPath,
		DisplayName:  nodeName,
		LocationPath: relPath,
		LastUpdated:  info.ModTime().Format(time.RFC3339),
		Size:         n,
		Type:         types.NodeTypeFile,
	}, nil
}

// NormalizePath cleans and normalizes any incoming path string.
func (l *LocalFS) NormalizePath(path string) string {
	p := filepath.Clean(path)
	p = strings.ReplaceAll(p, "\\", "/")
	return strings.TrimSuffix(p, "/")
}

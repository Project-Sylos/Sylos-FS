// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package local implements FSAdapter for the local filesystem with Lstat-first safety.
package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/fs/ctxstream"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// LocalFS implements types.FSAdapter for the local filesystem.
type LocalFS struct {
	root string

	OnWarning func(msg string)

	// PageCacheHints, when true, uses posix_fadvise (where supported) to hint
	// sequential read on open and drop cache after read close—reduces page cache
	// retention during migration without root. No effect on Windows/macOS stub.
	PageCacheHints bool

	warnState    warnState
	rootDev      uint64
	rootDevValid bool
}

// NewLocalFS constructs a new LocalFS adapter rooted at the given path.
func NewLocalFS(rootPath string) (*LocalFS, error) {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	abs = strings.ReplaceAll(filepath.Clean(abs), "\\", "/")
	l := &LocalFS{root: abs}
	if fi, err := os.Stat(abs); err == nil {
		if dev, ok := deviceID(fi); ok {
			l.rootDev = dev
			l.rootDevValid = true
		}
	}
	return l, nil
}

func (l *LocalFS) relativize(nodeName string, parentRelPath string) string {
	if parentRelPath == "/" {
		return "/" + nodeName
	}
	return parentRelPath + "/" + nodeName
}

// ListChildren lists immediate children; only directories and regular files (Lstat-gated).
func (l *LocalFS) ListChildren(identifier string, depth *int, parentPath string) (types.ListResult, error) {
	var result types.ListResult

	if isPseudoFSPath(l.root) {
		l.warnState.warnPseudoFS(l.OnWarning)
	}

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

	if _, err := listableDirInfo(identifier); err != nil {
		return result, err
	}

	entries, err := os.ReadDir(identifier)
	if err != nil {
		return result, err
	}

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(identifier, name)
		fullPath = strings.ReplaceAll(fullPath, "\\", "/")

		fi, err := os.Lstat(fullPath)
		if err != nil {
			continue
		}

		rel := l.relativize(name, parentRelPath)
		lastUpdated := fi.ModTime().Format(time.RFC3339)

		if childListableAsFolder(fi) {
			if l.rootDevValid {
				if dev, ok := deviceID(fi); ok && dev != l.rootDev {
					l.warnState.warnFsBoundary(l.OnWarning, fullPath)
				}
			}
			result.Folders = append(result.Folders, types.Folder{
				ServiceID:    fullPath,
				ParentId:     identifier,
				ParentPath:   parentRelPath,
				DisplayName:  name,
				LocationPath: rel,
				LastUpdated:  lastUpdated,
				Type:         types.NodeTypeFolder,
			})
			continue
		}

		if childCopyableAsFile(fi) {
			result.Files = append(result.Files, types.File{
				ServiceID:    fullPath,
				ParentId:     identifier,
				ParentPath:   parentRelPath,
				DisplayName:  name,
				LocationPath: rel,
				LastUpdated:  lastUpdated,
				Size:         fi.Size(),
				Type:         types.NodeTypeFile,
			})
		}
	}

	return result, nil
}

// OpenRead opens a regular file only; wraps with ctxstream for cancellation.
func (l *LocalFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	fi, err := os.Lstat(fileID)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrNotRegularFile, fileID)
	}
	file, err := os.Open(fileID)
	if err != nil {
		return nil, err
	}
	if l.PageCacheHints {
		_ = fadviseSequential(file)
		return newFadviseReadCloser(ctx, file), nil
	}
	return ctxstream.NewReadCloser(ctx, file), nil
}

// CreateFolder creates a new folder under a parent absolute path.
func (l *LocalFS) CreateFolder(parentId, name string) (types.Folder, error) {
	fullPath := filepath.Join(parentId, name)
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")

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

	relPath := l.relativize(name, parentRelPath)

	return types.Folder{
		ServiceID:    fullPath,
		ParentId:     parentId,
		ParentPath:   parentRelPath,
		DisplayName:  name,
		LocationPath: relPath,
		LastUpdated:  info.ModTime().Format(time.RFC3339),
		Type:         types.NodeTypeFolder,
	}, nil
}

// CreateFile creates an empty file at the destination path.
func (l *LocalFS) CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (types.File, error) {
	fullPath := filepath.Join(parentID, name)
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")

	normalizedParentId := strings.ReplaceAll(parentID, "\\", "/")
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

	f, err := os.Create(fullPath)
	if err != nil {
		return types.File{}, err
	}
	if err := f.Close(); err != nil {
		return types.File{}, err
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return types.File{}, err
	}

	relPath := l.relativize(name, parentRelPath)

	return types.File{
		ServiceID:    fullPath,
		ParentId:     parentID,
		ParentPath:   parentRelPath,
		DisplayName:  name,
		LocationPath: relPath,
		LastUpdated:  info.ModTime().Format(time.RFC3339),
		Size:         size,
		Type:         types.NodeTypeFile,
	}, nil
}

// OpenWrite opens an existing regular file for writing (truncate).
func (l *LocalFS) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	fi, err := os.Lstat(fileID)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrNotRegularFile, fileID)
	}
	file, err := os.OpenFile(fileID, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	return ctxstream.NewWriteCloser(ctx, file), nil
}

// NormalizePath cleans and normalizes any incoming path string.
func (l *LocalFS) NormalizePath(path string) string {
	p := filepath.Clean(path)
	p = strings.ReplaceAll(p, "\\", "/")
	return strings.TrimSuffix(p, "/")
}

// Initialize is a no-op for LocalFS.
func (l *LocalFS) Initialize(_ []byte, _ string) error {
	return nil
}

// RegisterCredentials is a no-op for LocalFS.
func (l *LocalFS) RegisterCredentials(_ []byte, _ []byte, _ string) error {
	return nil
}

// HasValidCredentials always returns true for LocalFS.
func (l *LocalFS) HasValidCredentials() bool {
	return true
}

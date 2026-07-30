// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package local implements FSAdapter for the local filesystem with Lstat-first safety.
package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/fs/ctxstream"
	"codeberg.org/Sylos/Sylos-FS/pkg/pathutil"
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

	degradation *types.FSDegradationState
	types.ConcurrencyHint

	injectBeforeOp func(operation string, attempt int) error // tests only
}

// NewLocalFS constructs a new LocalFS adapter rooted at the given path.
func NewLocalFS(rootPath string) (*LocalFS, error) {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	abs = strings.ReplaceAll(filepath.Clean(abs), "\\", "/")
	l := &LocalFS{root: abs, degradation: types.NewFSDegradationState()}
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
func (l *LocalFS) ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (types.ListResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var result types.ListResult
	err := l.withClassifiedRetryCtx(ctx, "ListChildren", func() error {
		var innerErr error
		result, innerErr = l.listChildrenOnce(ctx, identifier)
		return innerErr
	})
	return result, err
}

func (l *LocalFS) listChildrenOnce(ctx context.Context, identifier string) (types.ListResult, error) {
	var result types.ListResult

	if isBlockedPath(l.root) {
		l.warnState.warnPseudoFS(l.OnWarning)
	}
	if isBlockedPath(identifier) {
		return result, errBlockedPath(identifier)
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

	dir, err := os.Open(identifier)
	if err != nil {
		return result, err
	}
	defer dir.Close()

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		entries, err := dir.ReadDir(readDirBatchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return result, err
		}
		if len(entries) == 0 {
			break
		}

		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			name := entry.Name()
			fullPath := filepath.Join(identifier, name)
			fullPath = strings.ReplaceAll(fullPath, "\\", "/")

			if isBlockedPath(fullPath) {
				l.warnState.warnBlockedChild(l.OnWarning, fullPath)
				continue
			}

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

		if errors.Is(err, io.EOF) || len(entries) < readDirBatchSize {
			break
		}
	}

	return result, nil
}

// OpenRead opens a regular file only; wraps with ctxstream for cancellation.
func (l *LocalFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	err := l.withClassifiedRetryCtx(ctx, "OpenRead", func() error {
		var innerErr error
		rc, innerErr = l.openReadOnce(ctx, fileID)
		return innerErr
	})
	return rc, err
}

func (l *LocalFS) openReadOnce(ctx context.Context, fileID string) (io.ReadCloser, error) {
	if isBlockedPath(fileID) {
		return nil, errBlockedPath(fileID)
	}
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
func (l *LocalFS) CreateFolder(ctx context.Context, parentId, name string, _ map[string]string) (types.Folder, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var folder types.Folder
	err := l.withClassifiedRetryCtx(ctx, "CreateFolder", func() error {
		var innerErr error
		folder, innerErr = l.createFolderOnce(parentId, name)
		return innerErr
	})
	return folder, err
}

func (l *LocalFS) createFolderOnce(parentId, name string) (types.Folder, error) {
	fullPath := filepath.Join(parentId, name)
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")
	if isBlockedPath(fullPath) {
		return types.Folder{}, errBlockedPath(fullPath)
	}

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

// DeleteNode removes a file or folder by absolute path.
func (l *LocalFS) DeleteNode(ctx context.Context, nodeID string, nodeType string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return l.withClassifiedRetryCtx(ctx, "DeleteNode", func() error {
		return l.deleteNodeOnce(nodeID, nodeType)
	})
}

// RenameNode renames a local file or folder via os.Rename.
func (l *LocalFS) RenameNode(ctx context.Context, parentServiceID, serviceID, newName, nodeType string) (types.RenameResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	_ = nodeType
	var out types.RenameResult
	err := l.withClassifiedRetryCtx(ctx, "RenameNode", func() error {
		from := strings.ReplaceAll(serviceID, "\\", "/")
		if isBlockedPath(from) {
			return errBlockedPath(from)
		}
		parent := strings.TrimSpace(parentServiceID)
		if parent == "" {
			parent = filepath.Dir(from)
		}
		parent = strings.ReplaceAll(parent, "\\", "/")
		to := filepath.Join(parent, newName)
		to = strings.ReplaceAll(to, "\\", "/")
		if isBlockedPath(to) {
			return errBlockedPath(to)
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		out = types.RenameResult{ServiceID: to, DisplayName: newName}
		return nil
	})
	return out, err
}

func (l *LocalFS) deleteNodeOnce(nodeID, nodeType string) error {
	if err := l.assertDeletePathAllowed(nodeID); err != nil {
		return err
	}

	cleanPath, err := filepath.Abs(nodeID)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	cleanPath = filepath.Clean(cleanPath)

	info, err := os.Lstat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("node not found: %s", cleanPath)
		}
		return err
	}

	switch nodeType {
	case types.NodeTypeFile:
		if info.IsDir() {
			return fmt.Errorf("expected file, found folder: %s", cleanPath)
		}
		return os.Remove(cleanPath)
	case types.NodeTypeFolder:
		if !info.IsDir() {
			return fmt.Errorf("expected folder, found file: %s", cleanPath)
		}
		return os.RemoveAll(cleanPath)
	default:
		return fmt.Errorf("unsupported node type: %s", nodeType)
	}
}

// CreateFile creates an empty file at the destination path.
func (l *LocalFS) CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (types.File, error) {
	var file types.File
	err := l.withClassifiedRetryCtx(ctx, "CreateFile", func() error {
		var innerErr error
		file, innerErr = l.createFileOnce(parentID, name, size)
		return innerErr
	})
	return file, err
}

func (l *LocalFS) createFileOnce(parentID, name string, size int64) (types.File, error) {
	fullPath := filepath.Join(parentID, name)
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")
	if isBlockedPath(fullPath) {
		return types.File{}, errBlockedPath(fullPath)
	}

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
	var wc io.WriteCloser
	err := l.withClassifiedRetryCtx(ctx, "OpenWrite", func() error {
		var innerErr error
		wc, innerErr = l.openWriteOnce(ctx, fileID)
		return innerErr
	})
	return wc, err
}

func (l *LocalFS) openWriteOnce(ctx context.Context, fileID string) (io.WriteCloser, error) {
	if isBlockedPath(fileID) {
		return nil, errBlockedPath(fileID)
	}
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

// SupportsResumableTransfer implements types.FSTransferRestartPolicy: local files can seek.
func (l *LocalFS) SupportsResumableTransfer() bool { return true }

// RequiresDeleteBeforeRestart implements types.FSTransferRestartPolicy: overwrite/seek is safe.
func (l *LocalFS) RequiresDeleteBeforeRestart() bool { return false }

// OpenWriteFromOffset opens an existing file and seeks to offset (fresh handle, no session handoff).
func (l *LocalFS) OpenWriteFromOffset(ctx context.Context, fileID string, offset int64) (io.WriteCloser, error) {
	if offset <= 0 {
		return l.OpenWrite(ctx, fileID)
	}
	var wc io.WriteCloser
	err := l.withClassifiedRetryCtx(ctx, "OpenWriteFromOffset", func() error {
		fi, err := os.Lstat(fileID)
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%w: %s", ErrNotRegularFile, fileID)
		}
		file, err := os.OpenFile(fileID, os.O_RDWR, 0644)
		if err != nil {
			return err
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			_ = file.Close()
			return err
		}
		wc = ctxstream.NewWriteCloser(ctx, file)
		return nil
	})
	return wc, err
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

func (l *LocalFS) assertDeletePathAllowed(targetPath string) error {
	ok, err := pathutil.WithinRoot(l.root, targetPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	if ok {
		return nil
	}
	absRoot, err := filepath.Abs(l.root)
	if err != nil {
		return fmt.Errorf("invalid adapter root: %w", err)
	}
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	return fmt.Errorf("path %s is outside allowed root %s", filepath.Clean(absTarget), filepath.Clean(absRoot))
}

// DegradationState implements types.FSDegradationReporter (no local signals emitted).
func (l *LocalFS) DegradationState() types.FSDegradationSnapshot {
	if l.degradation == nil {
		return types.FSDegradationSnapshot{}
	}
	return l.degradation.DegradationState()
}

// RecordSignal implements types.FSDegradationReporter.
func (l *LocalFS) RecordSignal(sig types.FSDegradationSignal) {
	if l.degradation != nil {
		l.degradation.RecordSignal(sig)
	}
}

// GetDegradationState returns shared degradation telemetry for this adapter.
func (l *LocalFS) GetDegradationState() *types.FSDegradationState {
	return l.degradation
}

const (
	localListPageMin     = 20
	localListPageMax     = 1000
	localListPageDefault = 100
)

// ListChildrenPagination implements types.FSListChildrenPagination.
func (l *LocalFS) ListChildrenPagination() types.ListChildrenPagination {
	return types.ListChildrenPagination{
		MinPageSize:                   localListPageMin,
		MaxPageSize:                   localListPageMax,
		DefaultPageSize:               localListPageDefault,
		PreferLargePagesUnderThrottle: false,
	}
}

var (
	_ types.FSAdapter               = (*LocalFS)(nil)
	_ types.FSDegradationReporter   = (*LocalFS)(nil)
	_ types.FSTransferRestartPolicy = (*LocalFS)(nil)
	_ types.FSResumableWrite        = (*LocalFS)(nil)
	_ types.FSStorageInfo           = (*LocalFS)(nil)
)

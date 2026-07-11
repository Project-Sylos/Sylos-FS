// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sftp

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/fs/ctxstream"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	pkgsftp "github.com/pkg/sftp"
)

// SftpFS implements types.FSAdapter over a remote SFTP server.
type SftpFS struct {
	types.ConcurrencyHint
	session   *Session
	root      types.Folder
	rootAbs   string
	client    *Client
	masterKey []byte
}

func (f *SftpFS) sftpClient() *pkgsftp.Client {
	return f.client.SFTP()
}

func (f *SftpFS) ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (types.ListResult, error) {
	_ = depth
	if ctx == nil {
		ctx = context.Background()
	}
	var result types.ListResult
	err := f.withClassifiedRetryCtx(ctx, "ListChildren", func() error {
		listPath := normalizeRemotePath(identifier)
		if listPath == "" || listPath == "root" {
			listPath = f.rootAbs
		}
		if ok, err := withinRoot(f.rootAbs, listPath); err != nil || !ok {
			return fmt.Errorf("sftp: list path outside root: %s", listPath)
		}

		entries, err := f.sftpClient().ReadDir(listPath)
		if err != nil {
			return err
		}

		parentRel := parentRelPath(f.rootAbs, listPath)
		if parentPath != "" {
			parentRel = types.NormalizeLocationPath(parentPath)
		}

		result = types.ListResult{}
		for _, entry := range entries {
			name := entry.Name()
			if name == "." || name == ".." {
				continue
			}
			fullPath := joinRemote(listPath, name)
			modTime := entry.ModTime()
			if modTime.IsZero() {
				modTime = time.Now().UTC()
			}
			lastUpdated := modTime.UTC().Format(time.RFC3339)
			rel := relativize(name, parentRel)

			if entry.IsDir() {
				result.Folders = append(result.Folders, types.Folder{
					ServiceID:    fullPath,
					ParentId:     listPath,
					ParentPath:   parentRel,
					DisplayName:  name,
					LocationPath: types.NormalizeLocationPath(rel),
					LastUpdated:  lastUpdated,
					Type:         types.NodeTypeFolder,
				})
				continue
			}
			if entry.Mode().IsRegular() {
				result.Files = append(result.Files, types.File{
					ServiceID:    fullPath,
					ParentId:     listPath,
					ParentPath:   parentRel,
					DisplayName:  name,
					LocationPath: types.NormalizeLocationPath(rel),
					LastUpdated:  lastUpdated,
					Size:         entry.Size(),
					Type:         types.NodeTypeFile,
				})
			}
		}
		return nil
	})
	return result, err
}

func (f *SftpFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var rc io.ReadCloser
	err := f.withClassifiedRetryCtx(ctx, "OpenRead", func() error {
		path := normalizeRemotePath(fileID)
		if ok, _ := withinRoot(f.rootAbs, path); !ok {
			return fmt.Errorf("sftp: read outside root: %s", path)
		}
		file, err := f.sftpClient().Open(path)
		if err != nil {
			return err
		}
		rc = ctxstream.NewReadCloser(ctx, file)
		return nil
	})
	return rc, err
}

func (f *SftpFS) CreateFolder(ctx context.Context, parentId, name string) (types.Folder, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var folder types.Folder
	err := f.withClassifiedRetryCtx(ctx, "CreateFolder", func() error {
		parent := normalizeRemotePath(parentId)
		fullPath := joinRemote(parent, name)
		if ok, _ := withinRoot(f.rootAbs, fullPath); !ok {
			return fmt.Errorf("sftp: create folder outside root: %s", fullPath)
		}
		if err := f.sftpClient().Mkdir(fullPath); err != nil {
			return err
		}
		info, err := f.sftpClient().Stat(fullPath)
		if err != nil {
			return err
		}
		parentRel := parentRelPath(f.rootAbs, parent)
		folder = types.Folder{
			ServiceID:    fullPath,
			ParentId:     parent,
			ParentPath:   parentRel,
			DisplayName:  name,
			LocationPath: types.NormalizeLocationPath(relativize(name, parentRel)),
			LastUpdated:  info.ModTime().UTC().Format(time.RFC3339),
			Type:         types.NodeTypeFolder,
		}
		return nil
	})
	return folder, err
}

func (f *SftpFS) CreateFile(ctx context.Context, parentID, name string, size int64, _ map[string]string) (types.File, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var file types.File
	err := f.withClassifiedRetryCtx(ctx, "CreateFile", func() error {
		parent := normalizeRemotePath(parentID)
		fullPath := joinRemote(parent, name)
		if ok, _ := withinRoot(f.rootAbs, fullPath); !ok {
			return fmt.Errorf("sftp: create file outside root: %s", fullPath)
		}
		out, err := f.sftpClient().Create(fullPath)
		if err != nil {
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		info, err := f.sftpClient().Stat(fullPath)
		if err != nil {
			return err
		}
		parentRel := parentRelPath(f.rootAbs, parent)
		file = types.File{
			ServiceID:    fullPath,
			ParentId:     parent,
			ParentPath:   parentRel,
			DisplayName:  name,
			LocationPath: types.NormalizeLocationPath(relativize(name, parentRel)),
			LastUpdated:  info.ModTime().UTC().Format(time.RFC3339),
			Size:         size,
			Type:         types.NodeTypeFile,
		}
		return nil
	})
	return file, err
}

func (f *SftpFS) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var wc io.WriteCloser
	err := f.withClassifiedRetryCtx(ctx, "OpenWrite", func() error {
		path := normalizeRemotePath(fileID)
		if ok, _ := withinRoot(f.rootAbs, path); !ok {
			return fmt.Errorf("sftp: write outside root: %s", path)
		}
		file, err := f.sftpClient().OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE)
		if err != nil {
			return err
		}
		wc = file
		return nil
	})
	return wc, err
}

func (f *SftpFS) DeleteNode(ctx context.Context, nodeID string, nodeType string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return f.withClassifiedRetryCtx(ctx, "DeleteNode", func() error {
		path := normalizeRemotePath(nodeID)
		if ok, _ := withinRoot(f.rootAbs, path); !ok {
			return fmt.Errorf("sftp: delete outside root: %s", path)
		}
		switch nodeType {
		case types.NodeTypeFile:
			return f.sftpClient().Remove(path)
		case types.NodeTypeFolder:
			return f.sftpClient().RemoveDirectory(path)
		default:
			return fmt.Errorf("sftp: unsupported node type %q", nodeType)
		}
	})
}

func (f *SftpFS) NormalizePath(path string) string {
	return types.NormalizeLocationPath(path)
}

func (f *SftpFS) Initialize(_ []byte, _ string) error {
	return nil
}

func (f *SftpFS) RegisterCredentials(credsData []byte, masterKey []byte, connectionID string) error {
	f.masterKey = masterKey
	_ = credsData
	_ = connectionID
	return nil
}

func (f *SftpFS) HasValidCredentials() bool {
	return f.session != nil && f.session.HasValidCredentials()
}

func (f *SftpFS) DegradationState() types.FSDegradationSnapshot {
	if f.session == nil || f.session.degradation == nil {
		return types.FSDegradationSnapshot{}
	}
	return f.session.degradation.DegradationState()
}

func (f *SftpFS) RecordSignal(signal types.FSDegradationSignal) {
	if f.session != nil && f.session.degradation != nil {
		f.session.degradation.RecordSignal(signal)
	}
}

func (f *SftpFS) ListChildrenPagination() types.ListChildrenPagination {
	return types.ListChildrenPagination{
		MinPageSize:     20,
		DefaultPageSize: 100,
		MaxPageSize:     500,
	}
}

func (f *SftpFS) withClassifiedRetryCtx(ctx context.Context, operation string, op func() error) error {
	var tracker *types.AmbiguousErrorTracker
	if f.session.degradation != nil {
		tracker = f.session.degradation.AmbiguousTracker()
	}
	return credentials.DoWithClassifiedRetry(ctx, credentials.ClassifiedRetryConfig{
		RetryConfig: credentials.RetryConfig{
			MaxIterations:         32,
			MaxRateLimitWaits:     8,
			MaxRateLimitSleep:     5 * time.Second,
			DefaultRateLimitSleep: 250 * time.Millisecond,
			OnRateLimitWait: func(retryAfter time.Duration, attempt int) {
				f.RecordSignal(types.FSDegradationSignal{
					Kind:       types.FSDegradationRateLimit,
					RetryAfter: retryAfter,
					Operation:  operation,
					At:         time.Now(),
				})
			},
		},
		Operation:        operation,
		Classify:         classifySftpError,
		AmbiguousTracker: tracker,
		WorkerCount:      f.ActiveWorkers,
		OnSuspectedThrottle: func(class types.FSErrorClassification, attempt int) {
			f.RecordSignal(types.FSDegradationSignal{
				Kind:       types.FSDegradationSuspectedRateLimit,
				RetryAfter: 250 * time.Millisecond,
				Operation:  operation,
				At:         time.Now(),
			})
		},
	}, op)
}

var (
	_ types.FSDegradationReporter    = (*SftpFS)(nil)
	_ types.FSListChildrenPagination = (*SftpFS)(nil)
)

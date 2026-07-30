// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package box

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/fs/ctxstream"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// BoxFS implements FSAdapter for Box.
type BoxFS struct {
	types.ConcurrencyHint
	session   *Session
	root      types.Folder
	folderID  string
	masterKey []byte
}

func (d *BoxFS) resolveFolderID(identifier string) string {
	id := strings.TrimSpace(identifier)
	if id == "" || id == "root" || id == d.root.ServiceID {
		if d.folderID != "" {
			return d.folderID
		}
		return rootFolderID
	}
	return id
}

func (d *BoxFS) ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (types.ListResult, error) {
	_ = depth
	if ctx == nil {
		ctx = context.Background()
	}

	var result types.ListResult
	err := d.withClassifiedRetry(ctx, "ListChildren", func() error {
		client, err := d.session.apiClient(ctx)
		if err != nil {
			return err
		}
		entries, err := client.ListFolderItemsAll(ctx, d.resolveFolderID(identifier))
		if err != nil {
			return err
		}
		basePath := types.ListChildrenBasePath(d.root.LocationPath, parentPath)
		result = types.ListResult{}
		for _, e := range entries {
			switch e.Type {
			case "folder":
				result.Folders = append(result.Folders, d.itemToFolder(e, basePath))
			case "file":
				result.Files = append(result.Files, d.itemToFile(e, basePath))
			}
		}
		return nil
	})
	return result, err
}

func (d *BoxFS) itemToFolder(item Item, basePath string) types.Folder {
	loc := path.Join(basePath, item.Name)
	return types.Folder{
		ServiceID:    item.ID,
		ParentId:     parentIDOf(item),
		ParentPath:   basePath,
		DisplayName:  item.Name,
		LocationPath: types.NormalizeLocationPath(loc),
		LastUpdated:  item.ModifiedAt,
		Type:         types.NodeTypeFolder,
	}
}

func (d *BoxFS) itemToFile(item Item, basePath string) types.File {
	loc := path.Join(basePath, item.Name)
	return types.File{
		ServiceID:    item.ID,
		ParentId:     parentIDOf(item),
		ParentPath:   basePath,
		DisplayName:  item.Name,
		LocationPath: types.NormalizeLocationPath(loc),
		LastUpdated:  item.ModifiedAt,
		Size:         item.Size,
		Type:         types.NodeTypeFile,
	}
}

func (d *BoxFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	err := d.withClassifiedRetry(ctx, "OpenRead", func() error {
		client, err := d.session.apiClient(ctx)
		if err != nil {
			return err
		}
		body, err := client.DownloadFile(ctx, fileID)
		if err != nil {
			return err
		}
		rc = ctxstream.NewReadCloser(ctx, body)
		return nil
	})
	return rc, err
}

func (d *BoxFS) CreateFolder(ctx context.Context, parentId, name string, metadata map[string]string) (types.Folder, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parentId == "" {
		parentId = d.folderID
	}
	var out types.Folder
	err := d.withClassifiedRetry(ctx, "CreateFolder", func() error {
		client, err := d.session.apiClient(ctx)
		if err != nil {
			return err
		}
		created, err := client.CreateFolder(ctx, parentId, name)
		if err != nil {
			return err
		}
		basePath := types.LogicalParentFromCreateMetadata(metadata, d.root.LocationPath)
		out = d.itemToFolder(created, basePath)
		return nil
	})
	return out, err
}

func (d *BoxFS) DeleteNode(ctx context.Context, nodeID, nodeType string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("box: node id is required")
	}
	return d.withClassifiedRetry(ctx, "DeleteNode", func() error {
		client, err := d.session.apiClient(ctx)
		if err != nil {
			return err
		}
		if nodeType == types.NodeTypeFolder || nodeType == "folder" {
			return client.DeleteFolder(ctx, nodeID)
		}
		return client.DeleteFile(ctx, nodeID)
	})
}

func (d *BoxFS) CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (types.File, error) {
	_ = ctx
	if parentID == "" {
		parentID = d.folderID
	}
	basePath := types.LogicalParentFromCreateMetadata(metadata, d.root.LocationPath)
	loc := types.ChildLocationFromCreateMetadata(metadata, basePath, name)
	return types.File{
		ServiceID:    pendingFileID(parentID, name, size),
		ParentId:     parentID,
		ParentPath:   basePath,
		DisplayName:  name,
		LocationPath: loc,
		LastUpdated:  time.Now().UTC().Format(time.RFC3339),
		Size:         size,
		Type:         types.NodeTypeFile,
	}, nil
}

func (d *BoxFS) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	return d.OpenWriteWithSize(ctx, fileID, -1)
}

// OpenWriteWithSize streams an upload with a declared content length (required for
// chunked Box sessions and overwrite when the pending id has no size).
func (d *BoxFS) OpenWriteWithSize(ctx context.Context, fileID string, size int64) (io.WriteCloser, error) {
	w, err := newBoxWriter(d, ctx, fileID)
	if err != nil {
		return nil, err
	}
	if size >= 0 {
		w.size = size
	}
	return w, nil
}

func (d *BoxFS) NormalizePath(p string) string {
	return types.NormalizeLocationPath(p)
}

func (d *BoxFS) Initialize(masterKey []byte, connectionID string) error {
	d.masterKey = masterKey
	_ = connectionID
	return nil
}

func (d *BoxFS) RegisterCredentials(credsData []byte, masterKey []byte, connectionID string) error {
	if len(credsData) == 0 {
		return fmt.Errorf("box: empty credentials")
	}
	var stored cloud.StoredCredentials
	if err := json.Unmarshal(credsData, &stored); err != nil {
		return err
	}
	stored.Provider = cloud.ProviderBox
	if _, err := cloud.EncryptStoredCredentials(stored, masterKey, connectionID); err != nil {
		return err
	}
	d.session.mu.Lock()
	d.session.stored = stored
	d.session.mu.Unlock()
	d.masterKey = masterKey
	return nil
}

func (d *BoxFS) HasValidCredentials() bool {
	return d.session.HasValidCredentials()
}

func (d *BoxFS) DegradationState() types.FSDegradationSnapshot {
	if d.session.degradation == nil {
		return types.FSDegradationSnapshot{}
	}
	return d.session.degradation.DegradationState()
}

func (d *BoxFS) GetDegradationState() *types.FSDegradationState {
	return d.session.degradation
}

func (d *BoxFS) RecordSignal(signal types.FSDegradationSignal) {
	if d.session.degradation != nil {
		d.session.degradation.RecordSignal(signal)
	}
}

func (d *BoxFS) ListChildrenPagination() types.ListChildrenPagination {
	return types.ListChildrenPagination{
		MinPageSize:                   100,
		MaxPageSize:                   DefaultListLimit,
		DefaultPageSize:               DefaultListLimit,
		PreferLargePagesUnderThrottle: true,
	}
}

var (
	_ types.FSAdapter                = (*BoxFS)(nil)
	_ types.FSDegradationReporter    = (*BoxFS)(nil)
	_ types.FSListChildrenPagination = (*BoxFS)(nil)
	_ types.FSStorageInfo            = (*BoxFS)(nil)
)

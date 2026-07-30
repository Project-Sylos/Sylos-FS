// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/fs/ctxstream"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

const pendingFilePrefix = "pending:"

// ItemToFolder maps a Graph driveItem folder to types.Folder.
// ParentId carries the Graph drive id when known so browse can preserve drive context.
func ItemToFolder(item DriveItem, basePath, driveID string) types.Folder {
	loc := path.Join(basePath, item.Name)
	itemDrive := strings.TrimSpace(driveID)
	if item.ParentReference != nil && item.ParentReference.DriveID != "" {
		itemDrive = item.ParentReference.DriveID
	}
	return types.Folder{
		ServiceID:    item.ID,
		ParentId:     itemDrive,
		ParentPath:   basePath,
		DisplayName:  item.Name,
		LocationPath: types.NormalizeLocationPath(loc),
		LastUpdated:  item.LastModifiedDateTime,
		Type:         types.NodeTypeFolder,
	}
}

// ItemToFile maps a Graph driveItem file to types.File.
func ItemToFile(item DriveItem, basePath, driveID string) types.File {
	loc := path.Join(basePath, item.Name)
	itemDrive := strings.TrimSpace(driveID)
	if item.ParentReference != nil && item.ParentReference.DriveID != "" {
		itemDrive = item.ParentReference.DriveID
	}
	return types.File{
		ServiceID:    item.ID,
		ParentId:     itemDrive,
		ParentPath:   basePath,
		DisplayName:  item.Name,
		LocationPath: types.NormalizeLocationPath(loc),
		LastUpdated:  item.LastModifiedDateTime,
		Size:         item.Size,
		Type:         types.NodeTypeFile,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// DriveContext describes which drive/item an adapter is rooted at.
type DriveContext struct {
	RootType string
	DriveID  string
	FolderID string
	SiteID   string
}

// SessionAuth provides Graph HTTP access and token refresh for adapters.
type SessionAuth interface {
	GraphClient(ctx context.Context) (*Client, error)
	RefreshAccessToken(ctx context.Context) error
	ClearAccessToken()
	Degradation() *types.FSDegradationState
	ConnectionID() string
}

// AdapterOps is the shared drive-item FS surface used by OneDrive and SharePoint.
type AdapterOps struct {
	types.ConcurrencyHint
	Auth   SessionAuth
	Root   types.Folder
	Ctx    DriveContext
	Master []byte
}

func (a *AdapterOps) Client(ctx context.Context) (*Client, error) {
	return a.Auth.GraphClient(ctx)
}

func (a *AdapterOps) EffectiveDriveID() string {
	return strings.TrimSpace(a.Ctx.DriveID)
}

func (a *AdapterOps) EffectiveFolderID(identifier string) string {
	if strings.TrimSpace(identifier) != "" {
		return identifier
	}
	if a.Ctx.FolderID != "" {
		return a.Ctx.FolderID
	}
	return "root"
}

func (a *AdapterOps) ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (types.ListResult, error) {
	_ = depth
	if ctx == nil {
		ctx = context.Background()
	}
	parentID := a.EffectiveFolderID(identifier)
	// Opening a drive by its drive id (SharePoint library / namespaced root) lists the drive root.
	if driveID := a.EffectiveDriveID(); driveID != "" && parentID == driveID {
		parentID = "root"
	}
	var result types.ListResult
	err := a.WithClassifiedRetry(ctx, "ListChildren", func() error {
		client, err := a.Client(ctx)
		if err != nil {
			return err
		}
		items, err := client.ListChildren(ctx, ChildrenPath(a.EffectiveDriveID(), parentID), DefaultListPageSize)
		if err != nil {
			return err
		}
		basePath := types.ListChildrenBasePath(a.Root.LocationPath, parentPath)
		driveID := a.EffectiveDriveID()
		result = types.ListResult{}
		for _, item := range items {
			if item.Folder != nil {
				result.Folders = append(result.Folders, ItemToFolder(item, basePath, driveID))
				continue
			}
			if item.File != nil || item.Folder == nil {
				result.Files = append(result.Files, ItemToFile(item, basePath, driveID))
			}
		}
		return nil
	})
	return result, err
}

func (a *AdapterOps) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	err := a.WithClassifiedRetry(ctx, "OpenRead", func() error {
		client, err := a.Client(ctx)
		if err != nil {
			return err
		}
		body, err := client.OpenDownload(ctx, ItemPath(a.EffectiveDriveID(), fileID))
		if err != nil {
			return err
		}
		rc = ctxstream.NewReadCloser(ctx, body)
		return nil
	})
	return rc, err
}

func (a *AdapterOps) CreateFolder(ctx context.Context, parentID, name string, metadata map[string]string) (types.Folder, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parentID == "" {
		parentID = a.EffectiveFolderID("")
	}
	var out types.Folder
	err := a.WithClassifiedRetry(ctx, "CreateFolder", func() error {
		client, err := a.Client(ctx)
		if err != nil {
			return err
		}
		created, err := client.CreateFolder(ctx, ChildrenPath(a.EffectiveDriveID(), parentID), name)
		if err != nil {
			return err
		}
		basePath := types.LogicalParentFromCreateMetadata(metadata, a.Root.LocationPath)
		out = ItemToFolder(created, basePath, a.EffectiveDriveID())
		return nil
	})
	return out, err
}

func (a *AdapterOps) DeleteNode(ctx context.Context, nodeID, nodeType string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("msgraph: node id is required")
	}
	_ = nodeType
	return a.WithClassifiedRetry(ctx, "DeleteNode", func() error {
		client, err := a.Client(ctx)
		if err != nil {
			return err
		}
		return client.DeleteItem(ctx, ItemPath(a.EffectiveDriveID(), nodeID))
	})
}

func (a *AdapterOps) CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (types.File, error) {
	if parentID == "" {
		parentID = a.EffectiveFolderID("")
	}
	basePath := types.LogicalParentFromCreateMetadata(metadata, a.Root.LocationPath)
	loc := types.ChildLocationFromCreateMetadata(metadata, basePath, name)
	return types.File{
		ServiceID:    PendingFileID(parentID, name, size),
		ParentId:     firstNonEmpty(a.EffectiveDriveID(), parentID),
		ParentPath:   basePath,
		DisplayName:  name,
		LocationPath: loc,
		LastUpdated:  time.Now().UTC().Format(time.RFC3339),
		Size:         size,
		Type:         types.NodeTypeFile,
	}, nil
}

func (a *AdapterOps) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	return a.OpenWriteWithSize(ctx, fileID, -1)
}

func (a *AdapterOps) OpenWriteWithSize(ctx context.Context, fileID string, size int64) (io.WriteCloser, error) {
	w, err := NewWriter(a, ctx, fileID)
	if err != nil {
		return nil, err
	}
	if size >= 0 && w.declaredSize < 0 {
		w.declaredSize = size
		if size <= SimpleUploadMaxBytes {
			w.simpleOnly = true
		}
	}
	return w, nil
}

func (a *AdapterOps) NormalizePath(p string) string {
	return types.NormalizeLocationPath(p)
}

func (a *AdapterOps) DegradationState() types.FSDegradationSnapshot {
	if a.Auth.Degradation() == nil {
		return types.FSDegradationSnapshot{}
	}
	return a.Auth.Degradation().DegradationState()
}

func (a *AdapterOps) GetDegradationState() *types.FSDegradationState {
	return a.Auth.Degradation()
}

func (a *AdapterOps) RecordSignal(signal types.FSDegradationSignal) {
	if a.Auth.Degradation() != nil {
		a.Auth.Degradation().RecordSignal(signal)
	}
}

func (a *AdapterOps) ListChildrenPagination() types.ListChildrenPagination {
	return types.ListChildrenPagination{
		MinPageSize:                   20,
		DefaultPageSize:               DefaultListPageSize,
		MaxPageSize:                   DefaultListPageSize,
		PreferLargePagesUnderThrottle: false,
	}
}

func (a *AdapterOps) classifyError(err error) types.FSErrorClassification {
	class := ClassifyError(err)
	if class.Bucket != types.FSErrorThrottle {
		return class
	}
	class.RetryAfter = ThrottleBackoff(err, a.Auth.Degradation())
	return class
}

func (a *AdapterOps) WithClassifiedRetry(ctx context.Context, operation string, op func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var tracker *types.AmbiguousErrorTracker
	deg := a.Auth.Degradation()
	if deg != nil {
		tracker = deg.AmbiguousTracker()
	}
	return doClassifiedRetry(ctx, a, operation, tracker, op)
}

func (a *AdapterOps) recordDegradation(kind types.FSDegradationKind, operation string, retryAfter time.Duration) {
	if a.Auth.Degradation() == nil {
		return
	}
	a.Auth.Degradation().RecordSignal(types.FSDegradationSignal{
		Kind:       kind,
		RetryAfter: retryAfter,
		Operation:  operation,
		At:         time.Now(),
	})
}

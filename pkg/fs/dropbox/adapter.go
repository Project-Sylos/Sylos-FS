// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// DropboxFS implements FSAdapter for Dropbox.
type DropboxFS struct {
	types.ConcurrencyHint
	session   *Session
	root      types.Folder
	ctx       dropboxContext
	masterKey []byte
}

func (d *DropboxFS) client(ctx context.Context) (*Client, error) {
	return d.session.apiClient(ctx, d.pathRootForAPI())
}

// pathRootForAPI selects the Dropbox-API-Path-Root namespace, if any.
// Member-folder ("My Dropbox") operations use Dropbox default rooting without the header.
// Forcing home_namespace_id breaks App-folder OAuth apps (path/no_write_permission on /).
func (d *DropboxFS) pathRootForAPI() string {
	switch d.ctx.RootType {
	case cloud.RootTypeUserRoot:
		return ""
	case cloud.RootTypeSharedFolder:
		if d.ctx.RootPath != "" {
			return ""
		}
		return strings.TrimSpace(d.ctx.NamespaceID)
	case cloud.RootTypeTeamSpace, cloud.RootTypeTeamFolder:
		return strings.TrimSpace(d.ctx.NamespaceID)
	default:
		return strings.TrimSpace(d.ctx.NamespaceID)
	}
}

func (d *DropboxFS) errTeamSpaceRootWrite() error {
	if d.ctx.RootType == cloud.RootTypeTeamSpace {
		return fmt.Errorf("dropbox: cannot create files or folders at team space root; select My Dropbox or a team folder as the destination")
	}
	return nil
}

func (d *DropboxFS) listPath(identifier string) string {
	id := strings.TrimSpace(identifier)
	if isVirtualRootType(d.ctx.RootType) {
		switch d.ctx.RootType {
		case cloud.RootTypeSharedFolder:
			if d.ctx.RootPath != "" && (id == "" || id == "root" || id == d.root.ServiceID) {
				return d.ctx.RootPath
			}
		default:
			if id == "" || id == "root" || id == d.ctx.NamespaceID {
				return ""
			}
		}
	}
	if id != "" {
		return dropboxPathRef(id)
	}
	if d.ctx.FolderRef != "" {
		return dropboxPathRef(d.ctx.FolderRef)
	}
	if d.ctx.RootPath != "" {
		return d.ctx.RootPath
	}
	return ""
}

func (d *DropboxFS) ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (types.ListResult, error) {
	_ = depth
	if ctx == nil {
		ctx = context.Background()
	}
	listPath := d.listPath(identifier)

	var result types.ListResult
	err := d.withClassifiedRetry(ctx, "ListChildren", func() error {
		client, err := d.client(ctx)
		if err != nil {
			return err
		}
		entries, err := client.listFolderAll(ctx, listPath)
		if err != nil {
			return err
		}
		basePath := types.ListChildrenBasePath(d.root.LocationPath, parentPath)
		result = types.ListResult{}
		for _, e := range entries {
			if e.Tag == "folder" {
				result.Folders = append(result.Folders, d.metaToFolder(e, basePath))
				continue
			}
			if e.Tag == "file" {
				result.Files = append(result.Files, d.metaToFile(e, basePath))
			}
		}
		return nil
	})
	return result, err
}

func (d *DropboxFS) metaToFolder(m fileMetadata, basePath string) types.Folder {
	loc := path.Join(basePath, m.Name)
	return types.Folder{
		ServiceID:    m.ID,
		ParentId:     "",
		ParentPath:   basePath,
		DisplayName:  m.Name,
		LocationPath: types.NormalizeLocationPath(loc),
		LastUpdated:  modTime(m),
		Type:         types.NodeTypeFolder,
	}
}

func (d *DropboxFS) metaToFile(m fileMetadata, basePath string) types.File {
	loc := path.Join(basePath, m.Name)
	return types.File{
		ServiceID:    m.ID,
		ParentId:     "",
		ParentPath:   basePath,
		DisplayName:  m.Name,
		LocationPath: types.NormalizeLocationPath(loc),
		LastUpdated:  modTime(m),
		Size:         int64(m.Size),
		Type:         types.NodeTypeFile,
	}
}

func folderBasePath(meta fileMetadata, fallback string) string {
	if meta.PathDisplay != "" {
		return path.Dir(meta.PathDisplay)
	}
	if meta.PathLower != "" {
		return path.Dir(meta.PathLower)
	}
	return fallback
}

func modTime(m fileMetadata) string {
	if m.ServerMod != "" {
		return m.ServerMod
	}
	return m.ClientMod
}

func (d *DropboxFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	err := d.withClassifiedRetry(ctx, "OpenRead", func() error {
		client, err := d.client(ctx)
		if err != nil {
			return err
		}
		body, err := client.download(ctx, dropboxPathRef(fileID))
		if err != nil {
			return err
		}
		rc = streamDownload(ctx, body)
		return nil
	})
	return rc, err
}

func (d *DropboxFS) normalizeParentForCreate(parentID string) string {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" || parentID == "root" || parentID == d.root.ServiceID {
		return ""
	}
	if isVirtualRootType(d.ctx.RootType) && parentID == d.ctx.NamespaceID {
		return ""
	}
	return parentID
}

func (d *DropboxFS) sharedRootPath() string {
	ref := d.listPath("")
	if strings.HasPrefix(ref, "/") {
		return ref
	}
	return ""
}

// resolveLocationPath maps a migration root-relative path to a Dropbox API path.
func (d *DropboxFS) resolveLocationPath(locationPath string) string {
	loc := types.NormalizeLocationPath(locationPath)
	if loc == "" || loc == "/" {
		return loc
	}
	if root := d.sharedRootPath(); root != "" && root != "/" {
		if !strings.HasPrefix(loc, root+"/") && loc != root {
			return path.Join(root, strings.TrimPrefix(loc, "/"))
		}
	}
	return loc
}

func (d *DropboxFS) CreateFolder(ctx context.Context, parentId, name string, metadata map[string]string) (types.Folder, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := d.errTeamSpaceRootWrite(); err != nil && isDropboxRootRef(parentId) {
		return types.Folder{}, err
	}
	parentId = d.normalizeParentForCreate(parentId)
	var out types.Folder
	err := d.withClassifiedRetry(ctx, "CreateFolder", func() error {
		client, err := d.client(ctx)
		if err != nil {
			return err
		}
		folderPath, err := client.resolveCreatePath(ctx, parentId, name, d.sharedRootPath())
		if err != nil {
			return err
		}
		meta, err := client.createFolder(ctx, folderPath)
		if err != nil {
			return err
		}
		basePath := types.LogicalParentFromCreateMetadata(metadata, types.NormalizeLocationPath(d.root.LocationPath))
		if metadata == nil || (strings.TrimSpace(metadata["location_path"]) == "" && strings.TrimSpace(metadata["parent_path"]) == "") {
			basePath = folderBasePath(meta, basePath)
		}
		out = d.metaToFolder(meta, basePath)
		return nil
	})
	return out, err
}

func (d *DropboxFS) DeleteNode(ctx context.Context, nodeID string, nodeType string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("dropbox: node id is required")
	}
	_ = nodeType
	return d.withClassifiedRetry(ctx, "DeleteNode", func() error {
		client, err := d.client(ctx)
		if err != nil {
			return err
		}
		return client.deleteEntry(ctx, nodeID)
	})
}

func (d *DropboxFS) CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (types.File, error) {
	parentID = d.normalizeParentForCreate(parentID)
	if parentID == "" {
		if ref := d.listPath(""); ref != "" && strings.HasPrefix(ref, "/") {
			parentID = ref
		}
	}
	locPath := ""
	if metadata != nil {
		locPath = strings.TrimSpace(metadata["location_path"])
	}
	if locPath != "" {
		locPath = types.NormalizeLocationPath(locPath)
		parentPath := path.Dir(locPath)
		if parentPath == "." || parentPath == "" {
			parentPath = "/"
		}
		return types.File{
			ServiceID:    pendingFileByLocation(locPath),
			ParentId:     parentID,
			ParentPath:   types.NormalizeLocationPath(parentPath),
			DisplayName:  name,
			LocationPath: locPath,
			LastUpdated:  time.Now().UTC().Format(time.RFC3339),
			Size:         size,
			Type:         types.NodeTypeFile,
		}, nil
	}
	basePath := types.NormalizeLocationPath(d.root.LocationPath)
	loc := path.Join(basePath, name)
	return types.File{
		ServiceID:    pendingFileID(parentID, name),
		ParentId:     parentID,
		ParentPath:   basePath,
		DisplayName:  name,
		LocationPath: types.NormalizeLocationPath(loc),
		LastUpdated:  time.Now().UTC().Format(time.RFC3339),
		Size:         size,
		Type:         types.NodeTypeFile,
	}, nil
}

func (d *DropboxFS) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	return newDropboxWriter(d, ctx, fileID)
}

func (d *DropboxFS) NormalizePath(p string) string {
	return types.NormalizeLocationPath(p)
}

func (d *DropboxFS) Initialize(masterKey []byte, connectionID string) error {
	d.masterKey = masterKey
	_ = connectionID
	return nil
}

func (d *DropboxFS) RegisterCredentials(credsData []byte, masterKey []byte, connectionID string) error {
	if len(credsData) == 0 {
		return fmt.Errorf("dropbox: empty credentials")
	}
	var stored cloud.StoredCredentials
	if err := json.Unmarshal(credsData, &stored); err != nil {
		return err
	}
	stored.Provider = cloud.ProviderDropbox
	enc, err := cloud.EncryptStoredCredentials(stored, masterKey, connectionID)
	if err != nil {
		return err
	}
	d.session.mu.Lock()
	d.session.stored = stored
	d.session.mu.Unlock()
	_ = enc
	return nil
}

func (d *DropboxFS) HasValidCredentials() bool {
	return d.session.HasValidCredentials()
}

func (d *DropboxFS) DegradationState() types.FSDegradationSnapshot {
	if d.session.degradation == nil {
		return types.FSDegradationSnapshot{}
	}
	return d.session.degradation.DegradationState()
}

func (d *DropboxFS) GetDegradationState() *types.FSDegradationState {
	return d.session.degradation
}

func (d *DropboxFS) RecordSignal(signal types.FSDegradationSignal) {
	if d.session.degradation != nil {
		d.session.degradation.RecordSignal(signal)
	}
}

func (d *DropboxFS) ListChildrenPagination() types.ListChildrenPagination {
	return types.ListChildrenPagination{
		MinPageSize:                   20,
		DefaultPageSize:               100,
		MaxPageSize:                   500,
		PreferLargePagesUnderThrottle: false,
	}
}

var (
	_ types.FSDegradationReporter    = (*DropboxFS)(nil)
	_ types.FSListChildrenPagination = (*DropboxFS)(nil)
)

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

// DriveFS implements FSAdapter for Google Drive.
type DriveFS struct {
	types.ConcurrencyHint
	session   *Session
	root      types.Folder
	ctx       driveContext
	masterKey []byte
}

func (d *DriveFS) ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (types.ListResult, error) {
	_ = depth
	_ = parentPath
	if ctx == nil {
		ctx = context.Background()
	}
	parentID := identifier
	if parentID == "" {
		parentID = d.ctx.FolderID
	}

	var result types.ListResult
	err := d.withClassifiedRetry(ctx, "ListChildren", func() error {
		srv, err := d.session.driveService(ctx)
		if err != nil {
			return err
		}
		call := srv.Files.List().
			Q(d.listQuery(parentID)).
			Fields(googleapi.Field(listChildrenFields())).
			PageSize(100).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true)
		if d.ctx.RootType == cloud.RootTypeSharedDrive && d.ctx.DriveID != "" {
			call = call.DriveId(d.ctx.DriveID).Corpora("drive")
		}
		if d.ctx.RootType == cloud.RootTypeSharedWithMe && parentID == "sharedWithMe" {
			call = srv.Files.List().
				Q("sharedWithMe=true and trashed=false").
				Fields(googleapi.Field(listChildrenFields())).
				PageSize(100).
				SupportsAllDrives(true).
				IncludeItemsFromAllDrives(true)
		}

		resp, err := call.Do()
		if err != nil {
			return err
		}

		basePath := types.ListChildrenBasePath(d.root.LocationPath, parentPath)

		result = types.ListResult{}
		for _, f := range resp.Files {
			if f.MimeType == mimeGoogleFolder {
				result.Folders = append(result.Folders, d.fileToFolder(f, basePath))
				continue
			}
			if !listableDriveFile(f) {
				continue
			}
			result.Files = append(result.Files, d.fileToFile(f, basePath))
		}
		return nil
	})
	return result, err
}

func (d *DriveFS) listQuery(parentID string) string {
	if d.ctx.RootType == cloud.RootTypeSharedWithMe && parentID == "sharedWithMe" {
		return "sharedWithMe=true and trashed=false"
	}
	if parentID == "" || parentID == "root" {
		return "'root' in parents and trashed=false"
	}
	return fmt.Sprintf("'%s' in parents and trashed=false", strings.ReplaceAll(parentID, "'", "\\'"))
}

func (d *DriveFS) fileToFolder(f *drive.File, basePath string) types.Folder {
	loc := path.Join(basePath, f.Name)
	return types.Folder{
		ServiceID:    f.Id,
		ParentId:     firstParent(f.Parents),
		ParentPath:   basePath,
		DisplayName:  f.Name,
		LocationPath: types.NormalizeLocationPath(loc),
		LastUpdated:  f.ModifiedTime,
		Type:         types.NodeTypeFolder,
	}
}

func (d *DriveFS) fileToFile(f *drive.File, basePath string) types.File {
	effectiveMIME := effectiveContentMIME(f)
	displayName := displayNameForExport(f.Name, effectiveMIME)
	loc := path.Join(basePath, displayName)
	return types.File{
		ServiceID:    f.Id,
		ParentId:     firstParent(f.Parents),
		ParentPath:   basePath,
		DisplayName:  displayName,
		LocationPath: types.NormalizeLocationPath(loc),
		LastUpdated:  f.ModifiedTime,
		Size:         f.Size,
		Type:         types.NodeTypeFile,
	}
}

func firstParent(parents []string) string {
	if len(parents) == 0 {
		return ""
	}
	return parents[0]
}

func (d *DriveFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	err := d.withClassifiedRetry(ctx, "OpenRead", func() error {
		srv, err := d.session.driveService(ctx)
		if err != nil {
			return err
		}
		body, err := d.openDriveFileContent(ctx, srv, fileID)
		if err != nil {
			return err
		}
		rc = streamDownload(ctx, body)
		return nil
	})
	return rc, err
}

func (d *DriveFS) CreateFolder(ctx context.Context, parentId, name string, metadata map[string]string) (types.Folder, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parentId == "" {
		parentId = d.ctx.FolderID
	}
	var out types.Folder
	err := d.withClassifiedRetry(ctx, "CreateFolder", func() error {
		srv, err := d.session.driveService(ctx)
		if err != nil {
			return err
		}
		meta := &drive.File{
			Name:     name,
			MimeType: mimeGoogleFolder,
			Parents:  []string{parentId},
		}
		call := srv.Files.Create(meta).SupportsAllDrives(true).Fields("id,name,modifiedTime,parents")
		if d.ctx.RootType == cloud.RootTypeSharedDrive && d.ctx.DriveID != "" {
			meta.DriveId = d.ctx.DriveID
		}
		created, err := call.Do()
		if err != nil {
			return err
		}
		basePath := types.LogicalParentFromCreateMetadata(metadata, d.root.LocationPath)
		out = d.fileToFolder(created, basePath)
		return nil
	})
	return out, err
}

func (d *DriveFS) DeleteNode(ctx context.Context, nodeID string, nodeType string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("google drive: node id is required")
	}
	_ = nodeType
	return d.withClassifiedRetry(ctx, "DeleteNode", func() error {
		srv, err := d.session.driveService(ctx)
		if err != nil {
			return err
		}
		return srv.Files.Delete(nodeID).SupportsAllDrives(true).Do()
	})
}

func (d *DriveFS) CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (types.File, error) {
	if parentID == "" {
		parentID = d.ctx.FolderID
	}
	basePath := types.LogicalParentFromCreateMetadata(metadata, d.root.LocationPath)
	loc := types.ChildLocationFromCreateMetadata(metadata, basePath, name)
	return types.File{
		ServiceID:    pendingFileID(parentID, name),
		ParentId:     parentID,
		ParentPath:   basePath,
		DisplayName:  name,
		LocationPath: loc,
		LastUpdated:  time.Now().UTC().Format(time.RFC3339),
		Size:         size,
		Type:         types.NodeTypeFile,
	}, nil
}

func (d *DriveFS) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	return newDriveWriter(d, ctx, fileID)
}

func (d *DriveFS) NormalizePath(p string) string {
	return types.NormalizeLocationPath(p)
}

func (d *DriveFS) Initialize(masterKey []byte, connectionID string) error {
	d.masterKey = masterKey
	_ = connectionID
	return nil
}

func (d *DriveFS) RegisterCredentials(credsData []byte, masterKey []byte, connectionID string) error {
	if len(credsData) == 0 {
		return fmt.Errorf("google drive: empty credentials")
	}
	var stored cloud.StoredCredentials
	if err := json.Unmarshal(credsData, &stored); err != nil {
		return err
	}
	stored.Provider = cloud.ProviderGoogleDrive
	enc, err := cloud.EncryptStoredCredentials(stored, masterKey, connectionID)
	if err != nil {
		return err
	}
	d.session.stored = stored
	d.session.mu.Lock()
	d.session.stored = stored
	d.session.mu.Unlock()
	_ = enc
	return nil
}

func (d *DriveFS) HasValidCredentials() bool {
	return d.session.HasValidCredentials()
}

func (d *DriveFS) DegradationState() types.FSDegradationSnapshot {
	if d.session.degradation == nil {
		return types.FSDegradationSnapshot{}
	}
	return d.session.degradation.DegradationState()
}

func (d *DriveFS) GetDegradationState() *types.FSDegradationState {
	return d.session.degradation
}

func (d *DriveFS) RecordSignal(signal types.FSDegradationSignal) {
	if d.session.degradation != nil {
		d.session.degradation.RecordSignal(signal)
	}
}

func (d *DriveFS) ListChildrenPagination() types.ListChildrenPagination {
	return types.ListChildrenPagination{
		MinPageSize:     20,
		DefaultPageSize: 100,
		MaxPageSize:     100,
		PreferLargePagesUnderThrottle: false,
	}
}

// Ensure DriveFS satisfies optional interfaces at compile time.
var (
	_ types.FSDegradationReporter = (*DriveFS)(nil)
	_ types.FSListChildrenPagination = (*DriveFS)(nil)
	_ types.FSStorageInfo         = (*DriveFS)(nil)
)

// RegisterCredentialsPayload builds stored credentials JSON from UI token POST.
func RegisterCredentialsPayload(refreshToken, clientID, clientSecret string, scopes []string) ([]byte, error) {
	stored := cloud.StoredCredentialsFromOAuthTenant(cloud.ProviderGoogleDrive, refreshToken, clientID, clientSecret, scopes, "")
	return json.Marshal(stored)
}

// PrimeAccessToken stores a UI-supplied access token in memory only.
func (s *Session) PrimeAccessToken(accessToken string, expiresInSec int64) {
	expiry := time.Time{}
	if expiresInSec > 0 {
		expiry = time.Now().Add(time.Duration(expiresInSec) * time.Second)
	}
	s.tokens.SetAccessToken(s.connectionID, accessToken, expiry)
}

// Wrap auth errors for classified retry.
func wrapAuth(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", credentials.ErrNeedsRefresh, err)
}

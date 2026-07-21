// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sharepoint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/fs/msgraph"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// SharePointFS implements FSAdapter for SharePoint document libraries via Graph.
type SharePointFS struct {
	ops msgraph.AdapterOps
}

func (d *SharePointFS) ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (types.ListResult, error) {
	if d.ops.Ctx.RootType == cloud.RootTypeSharePointSite {
		return d.listSiteContents(ctx, identifier, parentPath)
	}
	return d.ops.ListChildren(ctx, identifier, depth, parentPath)
}

func (d *SharePointFS) listSiteContents(ctx context.Context, identifier, parentPath string) (types.ListResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	siteID := identifier
	if siteID == "" || siteID == "root" {
		siteID = d.ops.Ctx.SiteID
	}
	if siteID == "" {
		siteID = d.ops.Root.ServiceID
	}
	var result types.ListResult
	err := d.ops.WithClassifiedRetry(ctx, "ListChildren", func() error {
		client, err := d.ops.Client(ctx)
		if err != nil {
			return err
		}
		basePath := types.ListChildrenBasePath(d.ops.Root.LocationPath, parentPath)
		result = types.ListResult{}

		drives, err := client.ListSiteDrives(ctx, siteID)
		if err != nil {
			return err
		}
		for _, drive := range drives {
			name := drive.Name
			if name == "" {
				name = drive.ID
			}
			loc := path.Join(basePath, name)
			result.Folders = append(result.Folders, types.Folder{
				ServiceID:    drive.ID,
				ParentId:     drive.ID,
				ParentPath:   basePath,
				DisplayName:  name,
				LocationPath: types.NormalizeLocationPath(loc),
				Type:         cloud.RootTypeSharePointDrive,
			})
		}

		subsites, err := client.ListSubsites(ctx, siteID)
		if err != nil {
			return err
		}
		for _, site := range subsites {
			name := site.DisplayName
			if name == "" {
				name = site.Name
			}
			if name == "" {
				name = site.ID
			}
			loc := path.Join(basePath, name)
			result.Folders = append(result.Folders, types.Folder{
				ServiceID:    site.ID,
				ParentPath:   basePath,
				DisplayName:  name + " (subsite)",
				LocationPath: types.NormalizeLocationPath(loc),
				Type:         cloud.RootTypeSharePointSite,
			})
		}
		return nil
	})
	return result, err
}

func (d *SharePointFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	return d.ops.OpenRead(ctx, fileID)
}

func (d *SharePointFS) CreateFolder(ctx context.Context, parentId, name string, metadata map[string]string) (types.Folder, error) {
	if d.ops.Ctx.RootType == cloud.RootTypeSharePointSite {
		return types.Folder{}, fmt.Errorf("sharepoint: create folder under a site is not supported; open a document library first")
	}
	return d.ops.CreateFolder(ctx, parentId, name, metadata)
}

func (d *SharePointFS) DeleteNode(ctx context.Context, nodeID, nodeType string) error {
	if d.ops.Ctx.RootType == cloud.RootTypeSharePointSite {
		return fmt.Errorf("sharepoint: delete under a site container is not supported")
	}
	return d.ops.DeleteNode(ctx, nodeID, nodeType)
}

func (d *SharePointFS) CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (types.File, error) {
	if d.ops.Ctx.RootType == cloud.RootTypeSharePointSite {
		return types.File{}, fmt.Errorf("sharepoint: create file under a site is not supported; open a document library first")
	}
	return d.ops.CreateFile(ctx, parentID, name, size, metadata)
}

func (d *SharePointFS) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	return d.ops.OpenWrite(ctx, fileID)
}

func (d *SharePointFS) NormalizePath(p string) string { return d.ops.NormalizePath(p) }

func (d *SharePointFS) Initialize(masterKey []byte, connectionID string) error {
	d.ops.Master = masterKey
	_ = connectionID
	return nil
}

func (d *SharePointFS) RegisterCredentials(credsData []byte, masterKey []byte, connectionID string) error {
	if len(credsData) == 0 {
		return fmt.Errorf("sharepoint: empty credentials")
	}
	var stored cloud.StoredCredentials
	if err := json.Unmarshal(credsData, &stored); err != nil {
		return err
	}
	stored.Provider = cloud.ProviderSharePoint
	if _, err := cloud.EncryptStoredCredentials(stored, masterKey, connectionID); err != nil {
		return err
	}
	sess, ok := d.ops.Auth.(*Session)
	if !ok {
		return fmt.Errorf("sharepoint: invalid session")
	}
	sess.mu.Lock()
	sess.stored = stored
	sess.mu.Unlock()
	return nil
}

func (d *SharePointFS) HasValidCredentials() bool {
	return d.ops.Auth.(*Session).HasValidCredentials()
}

func (d *SharePointFS) DegradationState() types.FSDegradationSnapshot {
	return d.ops.DegradationState()
}

func (d *SharePointFS) GetDegradationState() *types.FSDegradationState {
	return d.ops.GetDegradationState()
}

func (d *SharePointFS) RecordSignal(signal types.FSDegradationSignal) {
	d.ops.RecordSignal(signal)
}

func (d *SharePointFS) ListChildrenPagination() types.ListChildrenPagination {
	return d.ops.ListChildrenPagination()
}

var (
	_ types.FSDegradationReporter    = (*SharePointFS)(nil)
	_ types.FSListChildrenPagination = (*SharePointFS)(nil)
)

// RegisterCredentialsPayload builds stored credentials JSON from UI token POST.
func RegisterCredentialsPayload(refreshToken, clientID, clientSecret string, scopes []string) ([]byte, error) {
	stored := cloud.StoredCredentialsFromOAuth(cloud.ProviderSharePoint, refreshToken, clientID, clientSecret, scopes)
	return json.Marshal(stored)
}

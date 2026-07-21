// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package onedrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/fs/msgraph"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// OneDriveFS implements FSAdapter for Microsoft OneDrive via Graph.
type OneDriveFS struct {
	ops msgraph.AdapterOps
}

func (d *OneDriveFS) ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (types.ListResult, error) {
	if d.ops.Ctx.RootType == cloud.RootTypeSharedWithMe && (identifier == "" || identifier == "sharedWithMe") {
		return d.listSharedWithMe(ctx, parentPath)
	}
	return d.ops.ListChildren(ctx, identifier, depth, parentPath)
}

func (d *OneDriveFS) listSharedWithMe(ctx context.Context, parentPath string) (types.ListResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var result types.ListResult
	err := d.ops.WithClassifiedRetry(ctx, "ListChildren", func() error {
		client, err := d.ops.Client(ctx)
		if err != nil {
			return err
		}
		items, err := client.SharedWithMe(ctx)
		if err != nil {
			return err
		}
		basePath := types.ListChildrenBasePath(d.ops.Root.LocationPath, parentPath)
		result = types.ListResult{}
		for _, item := range items {
			if item.Folder != nil {
				result.Folders = append(result.Folders, msgraph.ItemToFolder(item, basePath, ""))
				continue
			}
			result.Files = append(result.Files, msgraph.ItemToFile(item, basePath, ""))
		}
		return nil
	})
	return result, err
}

func (d *OneDriveFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	return d.ops.OpenRead(ctx, fileID)
}

func (d *OneDriveFS) CreateFolder(ctx context.Context, parentId, name string, metadata map[string]string) (types.Folder, error) {
	return d.ops.CreateFolder(ctx, parentId, name, metadata)
}

func (d *OneDriveFS) DeleteNode(ctx context.Context, nodeID, nodeType string) error {
	return d.ops.DeleteNode(ctx, nodeID, nodeType)
}

func (d *OneDriveFS) CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (types.File, error) {
	return d.ops.CreateFile(ctx, parentID, name, size, metadata)
}

func (d *OneDriveFS) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	return d.ops.OpenWrite(ctx, fileID)
}

func (d *OneDriveFS) NormalizePath(p string) string { return d.ops.NormalizePath(p) }

func (d *OneDriveFS) Initialize(masterKey []byte, connectionID string) error {
	d.ops.Master = masterKey
	_ = connectionID
	return nil
}

func (d *OneDriveFS) RegisterCredentials(credsData []byte, masterKey []byte, connectionID string) error {
	if len(credsData) == 0 {
		return fmt.Errorf("onedrive: empty credentials")
	}
	var stored cloud.StoredCredentials
	if err := json.Unmarshal(credsData, &stored); err != nil {
		return err
	}
	stored.Provider = cloud.ProviderOneDrive
	if _, err := cloud.EncryptStoredCredentials(stored, masterKey, connectionID); err != nil {
		return err
	}
	sess, ok := d.ops.Auth.(*Session)
	if !ok {
		return fmt.Errorf("onedrive: invalid session")
	}
	sess.mu.Lock()
	sess.stored = stored
	sess.mu.Unlock()
	return nil
}

func (d *OneDriveFS) HasValidCredentials() bool {
	return d.ops.Auth.(*Session).HasValidCredentials()
}

func (d *OneDriveFS) DegradationState() types.FSDegradationSnapshot {
	return d.ops.DegradationState()
}

func (d *OneDriveFS) GetDegradationState() *types.FSDegradationState {
	return d.ops.GetDegradationState()
}

func (d *OneDriveFS) RecordSignal(signal types.FSDegradationSignal) {
	d.ops.RecordSignal(signal)
}

func (d *OneDriveFS) ListChildrenPagination() types.ListChildrenPagination {
	return d.ops.ListChildrenPagination()
}

var (
	_ types.FSDegradationReporter    = (*OneDriveFS)(nil)
	_ types.FSListChildrenPagination = (*OneDriveFS)(nil)
)

// RegisterCredentialsPayload builds stored credentials JSON from UI token POST.
func RegisterCredentialsPayload(refreshToken, clientID, clientSecret string, scopes []string) ([]byte, error) {
	stored := cloud.StoredCredentialsFromOAuth(cloud.ProviderOneDrive, refreshToken, clientID, clientSecret, scopes)
	return json.Marshal(stored)
}

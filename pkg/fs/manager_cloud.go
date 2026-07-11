// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// CloudConnectionOptions configures a new cloud connection registration.
type CloudConnectionOptions struct {
	ProviderID     string
	ConnectionID   string
	MigrationDir   string
	MasterKey      []byte
	CredentialsJSON []byte // StoredCredentials JSON plaintext before encryption
	AccessToken    string
	ExpiresInSec   int64
}

func (m *ServiceManager) loadCloudServices(cloudServices []types.CloudServiceConfig) error {
	for _, svc := range cloudServices {
		if svc.ID == "" {
			return fmt.Errorf("cloud service missing id")
		}
		if svc.ProviderID == "" {
			return fmt.Errorf("cloud service %s missing provider_id", svc.ID)
		}
		normalized := svc
		if normalized.Name == "" {
			normalized.Name = normalized.ID
		}
		def := serviceDefinition{
			ID:    normalized.ID,
			Name:  normalized.Name,
			Type:  types.ServiceTypeCloud,
			Cloud: &normalized,
		}
		m.setService(def.ID, def)
	}
	return nil
}

// RegisterCloudConnection encrypts refresh credentials and opens a ref-counted cloud session.
func (m *ServiceManager) RegisterCloudConnection(opts CloudConnectionOptions) (string, error) {
	if opts.ProviderID == "" {
		return "", fmt.Errorf("providerID required")
	}
	if opts.ConnectionID == "" {
		return "", fmt.Errorf("connectionID required")
	}
	if len(opts.MasterKey) == 0 && len(opts.CredentialsJSON) == 0 {
		return "", fmt.Errorf("credentials JSON required")
	}
	if len(opts.CredentialsJSON) == 0 {
		return "", fmt.Errorf("credentials JSON required")
	}

	var stored cloud.StoredCredentials
	if err := json.Unmarshal(opts.CredentialsJSON, &stored); err != nil {
		return "", fmt.Errorf("invalid credentials JSON: %w", err)
	}
	stored.Provider = opts.ProviderID

	// Credential persistence is owned by Sylos-API (encrypted blobs in the migration DuckDB).

	factory, err := cloud.Factory(opts.ProviderID)
	if err != nil {
		return "", err
	}
	session, err := factory.NewSession(opts.ConnectionID, stored, cloud.DefaultTokenStore, types.NewFSDegradationState())
	if err != nil {
		return "", err
	}
	if opts.AccessToken != "" {
		if primable, ok := session.(interface {
			PrimeAccessToken(string, int64)
		}); ok {
			primable.PrimeAccessToken(opts.AccessToken, opts.ExpiresInSec)
		} else {
			expiry := time.Time{}
			if opts.ExpiresInSec > 0 {
				expiry = time.Now().Add(time.Duration(opts.ExpiresInSec) * time.Second)
			}
			cloud.DefaultTokenStore.SetAccessToken(opts.ConnectionID, opts.AccessToken, expiry)
		}
	}

	conn := &serviceConnection{
		typ:           types.ServiceTypeCloud,
		cloud:         session,
		cloudProvider: opts.ProviderID,
		refCount:      0,
	}
	if err := m.replaceOrRegisterConnection(opts.ConnectionID, conn); err != nil {
		_ = session.Close()
		return "", err
	}
	return opts.ConnectionID, nil
}

// ListCloudRoots returns provider-specific browse roots for a connection.
func (m *ServiceManager) ListCloudRoots(ctx context.Context, providerID, connectionID string) ([]cloud.Root, error) {
	conn, exists := m.getConnection(connectionID)
	if !exists || conn.typ != types.ServiceTypeCloud || conn.cloud == nil {
		return nil, fmt.Errorf("cloud connection %s not found", connectionID)
	}
	if conn.cloudProvider != "" && conn.cloudProvider != providerID {
		return nil, fmt.Errorf("cloud connection %s is for provider %q, not %q", connectionID, conn.cloudProvider, providerID)
	}
	factory, err := cloud.Factory(providerID)
	if err != nil {
		return nil, err
	}
	return factory.ListRoots(ctx, conn.cloud)
}

// ListCloudChildren lists children for a cloud connection before migration root is set.
// rootType should match the cloud.RootType from GET .../roots when listing a virtual root.
// driveID is namespace metadata from /roots (Dropbox team_folder, shared_folder).
func (m *ServiceManager) ListCloudChildren(ctx context.Context, connectionID, identifier, rootType, driveID string, offset, limit int, foldersOnly bool) (types.ListResult, types.PaginationInfo, error) {
	conn, exists := m.getConnection(connectionID)
	if !exists || conn.typ != types.ServiceTypeCloud || conn.cloud == nil {
		return types.ListResult{}, types.PaginationInfo{}, fmt.Errorf("cloud connection %s not found", connectionID)
	}
	var folder types.Folder
	var err error
	if rootType != "" {
		folder, err = cloud.BrowseRoot(cloud.Root{ID: identifier, RootType: rootType, DriveID: driveID})
	} else {
		folder, err = cloud.BrowseFolder(identifier, "", driveID)
	}
	if err != nil {
		return types.ListResult{}, types.PaginationInfo{}, err
	}
	adapter, err := conn.cloud.CreateAdapter(folder)
	if err != nil {
		return types.ListResult{}, types.PaginationInfo{}, err
	}
	result, err := adapter.ListChildren(ctx, identifier, nil, "")
	if err != nil {
		return types.ListResult{}, types.PaginationInfo{}, err
	}
	paginated, pagination := applyPagination(result, offset, limit, foldersOnly)
	return paginated, pagination, nil
}

// RevokeCloudConnection removes in-memory session state.
func (m *ServiceManager) RevokeCloudConnection(connectionID, migrationDir string) error {
	_ = migrationDir
	conn, shouldDelete := m.decrementConnectionRefCount(connectionID)
	if conn != nil && shouldDelete && conn.cloud != nil {
		_ = conn.cloud.Close()
	}
	cloud.DefaultTokenStore.ClearAccessToken(connectionID)
	return nil
}

func (m *ServiceManager) acquireCloudAdapter(def serviceDefinition, root types.Folder, connectionID string) (types.FSAdapter, func(), error) {
	if def.Cloud == nil {
		return nil, nil, fmt.Errorf("cloud configuration missing")
	}
	conn, err := m.incrementConnectionRefCount(connectionID)
	if err != nil {
		return nil, nil, err
	}
	if conn.typ != types.ServiceTypeCloud || conn.cloud == nil {
		m.decrementConnectionRefCount(connectionID)
		return nil, nil, fmt.Errorf("connection %s is not a cloud session", connectionID)
	}

	folder := root
	if folder.Type == "" {
		folder.Type = types.NodeTypeFolder
	}
	if folder.ServiceID == "" && folder.ParentId == "" && folder.Type == types.NodeTypeFolder {
		folder.ServiceID = "root"
	}

	adapter, err := conn.cloud.CreateAdapter(folder)
	if err != nil {
		m.decrementConnectionRefCount(connectionID)
		return nil, nil, err
	}

	release := func() {
		if closedConn, shouldClose := m.decrementConnectionRefCount(connectionID); shouldClose && closedConn != nil && closedConn.cloud != nil {
			_ = closedConn.cloud.Close()
		}
	}
	return adapter, release, nil
}

// InitializeCloudAdapter loads encrypted credentials into an adapter.
func (m *ServiceManager) InitializeCloudAdapter(adapter types.FSAdapter, masterKey []byte, connectionID string) error {
	return adapter.Initialize(masterKey, connectionID)
}

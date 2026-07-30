// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package fs

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/fs/local"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// GetStorageInfoRequest asks for capacity / free space for a service root or account.
type GetStorageInfoRequest struct {
	ServiceID    string
	Path         string // local volume path; ignored for account-level cloud quotas
	ConnectionID string // required for cloud / Spectra
	RootType     string // cloud browse root type (optional)
	DriveID      string // cloud namespace / Graph drive id (optional)
	Role         string // spectra virtual service role (source/destination)
}

// GetStorageInfo returns best-effort storage capacity for the given service.
// Providers that cannot report capacity return Available=false without error.
func (m *ServiceManager) GetStorageInfo(ctx context.Context, req GetStorageInfoRequest) (types.StorageInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	serviceID := strings.TrimSpace(req.ServiceID)
	if serviceID == "" {
		return types.UnavailableStorage(), fmt.Errorf("serviceID required")
	}

	if serviceID == "local" {
		path := req.Path
		if path == "" {
			if runtime.GOOS == "windows" {
				path = `C:\`
			} else {
				path = "/"
			}
		}
		return local.FilesystemUsage(path)
	}

	if serviceID == "spectra" {
		return m.getSpectraStorageInfo(ctx, req)
	}

	def, err := m.GetServiceDefinition(serviceID)
	if err != nil {
		return types.UnavailableStorage(), err
	}

	switch def.Type {
	case types.ServiceTypeLocal:
		path := req.Path
		if path == "" && def.Local != nil {
			path = def.Local.RootPath
		}
		if path == "" {
			path = "/"
		}
		return local.FilesystemUsage(path)

	case types.ServiceTypeSpectra:
		return m.getSpectraStorageInfo(ctx, req)

	case types.ServiceTypeCloud:
		return m.getCloudStorageInfo(ctx, req)

	default:
		return types.UnavailableStorage(), fmt.Errorf("unsupported service type: %s", def.Type)
	}
}

func (m *ServiceManager) getSpectraStorageInfo(ctx context.Context, req GetStorageInfoRequest) (types.StorageInfo, error) {
	fake := types.StorageInfo{
		Available:  true,
		TotalBytes: 10 << 40,
		UsedBytes:  0,
		FreeBytes:  10 << 40,
		Source:     "spectra.fake",
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	world := "primary"
	if role == "destination" {
		world = "s1"
	}
	def, err := m.GetServiceDefinitionByWorld(world)
	if err != nil || req.ConnectionID == "" {
		return fake, nil
	}
	root := types.Folder{ServiceID: "root", Type: types.NodeTypeFolder, LocationPath: "/"}
	adapter, release, err := m.AcquireAdapter(def, root, req.ConnectionID)
	if err != nil {
		return fake, nil
	}
	defer release()
	si, ok := types.StorageInfoFrom(adapter)
	if !ok {
		return fake, nil
	}
	info, err := si.GetStorageInfo(ctx, req.Path)
	if err != nil {
		return fake, nil
	}
	return info, nil
}

func (m *ServiceManager) getCloudStorageInfo(ctx context.Context, req GetStorageInfoRequest) (types.StorageInfo, error) {
	if req.ConnectionID == "" {
		return types.UnavailableStorage(), fmt.Errorf("connectionID required for cloud storage info")
	}
	// Shared-with-me and similar virtual spaces are not owned capacity; do not
	// report the account quota as if it applied to that listing.
	if !cloud.ReportsOwnedStorageQuota(req.RootType) {
		return types.UnavailableStorage(), nil
	}
	conn, exists := m.getConnection(req.ConnectionID)
	if !exists || conn.typ != types.ServiceTypeCloud || conn.cloud == nil {
		return types.UnavailableStorage(), fmt.Errorf("cloud connection %s not found", req.ConnectionID)
	}
	folder, err := cloud.BrowseFolder("root", req.RootType, req.DriveID)
	if err != nil {
		folder = types.Folder{ServiceID: "root", Type: types.NodeTypeFolder, LocationPath: "/"}
		if req.RootType != "" {
			folder.Type = req.RootType
		}
		if req.DriveID != "" {
			folder.ParentId = req.DriveID
		}
	} else if req.DriveID != "" && folder.ParentId == "" {
		folder.ParentId = req.DriveID
	}
	adapter, err := conn.cloud.CreateAdapter(folder)
	if err != nil {
		return types.UnavailableStorage(), err
	}
	si, ok := types.StorageInfoFrom(adapter)
	if !ok {
		return types.UnavailableStorage(), nil
	}
	return si.GetStorageInfo(ctx, req.Path)
}

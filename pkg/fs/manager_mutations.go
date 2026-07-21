// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: LGPL-2.1-or-later

package fs

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/fs/local"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// CreateFolder creates a folder during browse setup.
func (m *ServiceManager) CreateFolder(ctx context.Context, req types.BrowseMutationRequest, parentID, name string) (types.Folder, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sanitized, err := sanitizeFolderName(name)
	if err != nil {
		return types.Folder{}, err
	}
	if strings.TrimSpace(parentID) == "" {
		return types.Folder{}, fmt.Errorf("parent id is required")
	}

	adapter, err := m.browseAdapter(ctx, req)
	if err != nil {
		return types.Folder{}, err
	}
	return adapter.CreateFolder(ctx, parentID, sanitized, nil)
}

// DeleteNodes deletes files and folders during browse setup.
func (m *ServiceManager) DeleteNodes(ctx context.Context, req types.BrowseMutationRequest, nodes []types.NodeRef) (types.DeleteNodesResult, error) {
	result := types.DeleteNodesResult{
		Deleted: make([]string, 0, len(nodes)),
		Errors:  make([]types.DeleteNodeError, 0),
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(nodes) == 0 {
		return result, fmt.Errorf("no nodes to delete")
	}

	adapter, err := m.browseAdapter(ctx, req)
	if err != nil {
		return result, err
	}

	for _, node := range nodes {
		if err := validateDeleteNode(node); err != nil {
			result.Errors = append(result.Errors, types.DeleteNodeError{
				ID:      node.ID,
				Message: err.Error(),
			})
			continue
		}
		if err := adapter.DeleteNode(ctx, node.ID, node.Type); err != nil {
			result.Errors = append(result.Errors, types.DeleteNodeError{
				ID:      node.ID,
				Message: err.Error(),
			})
			continue
		}
		result.Deleted = append(result.Deleted, node.ID)
	}
	return result, nil
}

func sanitizeFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("folder name is required")
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("invalid folder name")
	}
	return name, nil
}

func validateDeleteNode(node types.NodeRef) error {
	id := strings.TrimSpace(node.ID)
	if id == "" {
		return fmt.Errorf("node id is required")
	}
	switch node.Type {
	case types.NodeTypeFile, types.NodeTypeFolder:
	default:
		return fmt.Errorf("unsupported node type: %s", node.Type)
	}
	lower := strings.ToLower(id)
	if lower == "root" || lower == "/" || strings.HasPrefix(lower, "\\\\.\\") {
		return fmt.Errorf("cannot delete root: %s", id)
	}
	return nil
}

func (m *ServiceManager) browseAdapter(_ context.Context, req types.BrowseMutationRequest) (types.FSAdapter, error) {
	serviceID := req.ServiceID

	if serviceID == "spectra" {
		role := strings.ToLower(strings.TrimSpace(req.Role))
		var world string
		switch role {
		case "source":
			world = "primary"
		case "destination":
			world = "s1"
		default:
			world = "primary"
		}
		def, err := m.GetServiceDefinitionByWorld(world)
		if err != nil {
			return nil, err
		}
		if req.ConnectionID == "" {
			return nil, fmt.Errorf("spectra browse mutations require a session id")
		}
		conn, exists := m.getConnection(req.ConnectionID)
		if !exists || conn.typ != types.ServiceTypeSpectra || conn.session == nil {
			return nil, fmt.Errorf("spectra session %s not found", req.ConnectionID)
		}
		root := def.Spectra.RootID
		if root == "" {
			root = "root"
		}
		return conn.session.CreateAdapter(root, def.Spectra.World)
	}

	if serviceID == "local" {
		localDef, found := m.findLocalService()
		if !found {
			return nil, fmt.Errorf("no local filesystem services configured")
		}
		unrestrictedDef := localDef
		if unrestrictedDef.Local != nil {
			unrestrictedLocal := *unrestrictedDef.Local
			unrestrictedLocal.RootPath = ""
			unrestrictedDef.Local = &unrestrictedLocal
		}
		contextID := req.ContextID
		if contextID == "" {
			contextID = req.ConnectionID
		}
		return m.localBrowseAdapter(contextID, unrestrictedDef)
	}

	def, err := m.GetServiceDefinition(serviceID)
	if err != nil {
		return nil, err
	}

	switch def.Type {
	case types.ServiceTypeLocal:
		contextID := req.ContextID
		if contextID == "" {
			contextID = req.ConnectionID
		}
		return m.localBrowseAdapter(contextID, def)
	case types.ServiceTypeCloud:
		if req.ConnectionID == "" {
			return nil, fmt.Errorf("cloud browse mutations require connection id")
		}
		return m.cloudBrowseAdapter(req.ConnectionID, req.ContextID, req.RootType, req.DriveID)
	default:
		return nil, fmt.Errorf("unsupported service type for browse mutations: %s", def.Type)
	}
}

func (m *ServiceManager) localBrowseAdapter(identifier string, def serviceDefinition) (types.FSAdapter, error) {
	if def.Local == nil {
		return nil, fmt.Errorf("local service %s missing configuration", def.ID)
	}

	root := def.Local.RootPath
	if root == "" {
		target := identifier
		if target == "" {
			if runtime.GOOS == "windows" {
				target = "C:\\"
			} else {
				target = "/"
			}
		}
		cleanTarget, err := filepath.Abs(target)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve path: %w", err)
		}
		cleanTarget = filepath.Clean(cleanTarget)

		var adapterRoot string
		if runtime.GOOS == "windows" {
			drive := filepath.VolumeName(cleanTarget)
			if drive != "" {
				adapterRoot = drive + "\\"
			} else {
				adapterRoot = "C:\\"
			}
		} else {
			adapterRoot = "/"
		}
		return local.NewLocalFS(adapterRoot)
	}

	adapter, err := local.NewLocalFS(root)
	if err != nil {
		return nil, err
	}
	return adapter, nil
}

func (m *ServiceManager) cloudBrowseAdapter(connectionID, contextID, rootType, driveID string) (types.FSAdapter, error) {
	conn, exists := m.getConnection(connectionID)
	if !exists || conn.typ != types.ServiceTypeCloud || conn.cloud == nil {
		return nil, fmt.Errorf("cloud connection %s not found", connectionID)
	}
	if strings.TrimSpace(contextID) == "" {
		return nil, fmt.Errorf("browse context id is required")
	}
	if rootType == "" && contextID == "sharedWithMe" {
		rootType = cloud.RootTypeSharedWithMe
	}
	var folder types.Folder
	var err error
	if rootType != "" {
		folder, err = cloud.BrowseRoot(cloud.Root{ID: contextID, RootType: rootType, DriveID: driveID})
	} else {
		folder, err = cloud.BrowseFolder(contextID, "", driveID)
	}
	if err != nil {
		return nil, err
	}
	return conn.cloud.CreateAdapter(folder)
}

// CountChildren returns total child count for a folder (used for empty-source validation).
func (m *ServiceManager) CountChildren(ctx context.Context, req types.ListChildrenRequest) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req.Limit = 1
	req.Offset = 0
	req.FoldersOnly = false
	_, pagination, err := m.ListChildren(ctx, req)
	if err != nil {
		return 0, err
	}
	return pagination.Total, nil
}

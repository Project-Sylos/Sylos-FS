// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/Project-Sylos/Spectra/sdk"
	"github.com/Project-Sylos/Sylos-FS/pkg/types"
)

var ErrServiceNotFound = fmt.Errorf("service not found")

// ServiceManager handles service-related operations
type ServiceManager struct {
	services    map[string]serviceDefinition
	connections map[string]*serviceConnection
	mu          sync.RWMutex
}

type serviceDefinition = types.ServiceDefinition // alias for internal use

type serviceConnection struct {
	typ      types.ServiceType
	spectra  *sdk.SpectraFS
	refCount int
}

// NewServiceManager creates a new ServiceManager instance
func NewServiceManager() *ServiceManager {
	return &ServiceManager{
		services:    make(map[string]serviceDefinition),
		connections: make(map[string]*serviceConnection),
	}
}

// LoadServices loads services from the provided service definitions
func (m *ServiceManager) LoadServices(localServices []types.LocalServiceConfig, spectraServices []types.SpectraServiceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, svc := range localServices {
		if svc.ID == "" {
			return fmt.Errorf("local service missing id")
		}

		normalized := svc
		if normalized.Name == "" {
			normalized.Name = normalized.ID
		}

		if normalized.RootPath != "" {
			abs, err := filepath.Abs(normalized.RootPath)
			if err != nil {
				return fmt.Errorf("local service %s: %w", normalized.ID, err)
			}
			normalized.RootPath = filepath.Clean(abs)
		}

		def := serviceDefinition{
			ID:    normalized.ID,
			Name:  normalized.Name,
			Type:  types.ServiceTypeLocal,
			Local: &normalized,
		}

		m.services[def.ID] = def
	}

	for _, svc := range spectraServices {
		if svc.ID == "" {
			return fmt.Errorf("spectra service missing id")
		}

		normalized := svc
		if normalized.Name == "" {
			normalized.Name = normalized.ID
		}

		if normalized.World == "" {
			normalized.World = "primary"
		}

		if normalized.RootID == "" {
			normalized.RootID = "root"
		}

		if normalized.ConfigPath == "" {
			return fmt.Errorf("spectra service %s missing config_path", normalized.ID)
		}

		abs, err := filepath.Abs(normalized.ConfigPath)
		if err != nil {
			return fmt.Errorf("spectra service %s: %w", normalized.ID, err)
		}
		normalized.ConfigPath = filepath.Clean(abs)

		def := serviceDefinition{
			ID:      normalized.ID,
			Name:    normalized.Name,
			Type:    types.ServiceTypeSpectra,
			Spectra: &normalized,
		}

		m.services[def.ID] = def
	}

	return nil
}

// ListSources returns a list of all available sources
func (m *ServiceManager) ListSources(ctx context.Context) ([]types.Source, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sources := make([]types.Source, 0)
	hasSpectra := false
	var spectraConfigPath string

	for _, svc := range m.services {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		switch svc.Type {
		case types.ServiceTypeLocal:
			// Add all local services individually
			metadata := map[string]string{
				"name": svc.Name,
			}
			if svc.Local != nil && svc.Local.RootPath != "" {
				metadata["rootPath"] = svc.Local.RootPath
			}

			sources = append(sources, types.Source{
				ID:          svc.ID,
				DisplayName: svc.Name,
				Type:        svc.Type,
				Metadata:    metadata,
			})

		case types.ServiceTypeSpectra:
			// Track that we have Spectra services, but don't add them individually
			if !hasSpectra {
				if svc.Spectra != nil {
					spectraConfigPath = svc.Spectra.ConfigPath
				}
				hasSpectra = true
			}
		}
	}

	// Add a single "spectra" service entry if any Spectra services exist
	if hasSpectra {
		metadata := map[string]string{
			"name": "Spectra",
		}
		if spectraConfigPath != "" {
			metadata["configPath"] = spectraConfigPath
		}

		sources = append(sources, types.Source{
			ID:          "spectra",
			DisplayName: "Spectra",
			Type:        types.ServiceTypeSpectra,
			Metadata:    metadata,
		})
	}

	sort.Slice(sources, func(i, j int) bool {
		return sources[i].DisplayName < sources[j].DisplayName
	})

	return sources, nil
}

// applyPagination applies pagination to a ListResult and returns pagination metadata
func applyPagination(result types.ListResult, offset, limit int, foldersOnly bool) (types.ListResult, types.PaginationInfo) {
	// Normalize pagination parameters
	if limit <= 0 {
		limit = 100 // Default limit
	}
	if limit > 1000 {
		limit = 1000 // Max limit
	}
	if offset < 0 {
		offset = 0
	}

	totalFolders := len(result.Folders)
	totalFiles := len(result.Files)
	total := totalFolders + totalFiles

	// Initialize paginated result with empty slices
	paginatedResult := types.ListResult{
		Folders: []types.Folder{},
		Files:   []types.File{},
	}

	if foldersOnly {
		// Only paginate folders
		total = totalFolders
		if offset < totalFolders {
			end := offset + limit
			if end > totalFolders {
				end = totalFolders
			}
			// Explicitly slice the folders array
			paginatedResult.Folders = result.Folders[offset:end]
		}
		// Don't include files when foldersOnly is true
		paginatedResult.Files = []types.File{}
	} else {
		// Paginate all items (folders first, then files)
		allItems := make([]interface{}, 0, totalFolders+totalFiles)
		for i := range result.Folders {
			allItems = append(allItems, &result.Folders[i])
		}
		for i := range result.Files {
			allItems = append(allItems, &result.Files[i])
		}

		if offset < len(allItems) {
			end := offset + limit
			if end > len(allItems) {
				end = len(allItems)
			}

			// Separate back into folders and files
			paginatedResult.Folders = make([]types.Folder, 0)
			paginatedResult.Files = make([]types.File, 0)

			for i := offset; i < end; i++ {
				switch item := allItems[i].(type) {
				case *types.Folder:
					paginatedResult.Folders = append(paginatedResult.Folders, *item)
				case *types.File:
					paginatedResult.Files = append(paginatedResult.Files, *item)
				}
			}
		}
	}

	hasMore := (offset + limit) < total

	return paginatedResult, types.PaginationInfo{
		Offset:       offset,
		Limit:        limit,
		Total:        total,
		TotalFolders: totalFolders,
		TotalFiles:   totalFiles,
		HasMore:      hasMore,
	}
}

// paginatedListResult wraps a ListResult with pagination info
type paginatedListResult struct {
	Result     types.ListResult
	Pagination types.PaginationInfo
}

// ListChildren lists children of a service with pagination support
func (m *ServiceManager) ListChildren(ctx context.Context, req types.ListChildrenRequest) (types.ListResult, types.PaginationInfo, error) {
	serviceID := req.ServiceID

	// Map "spectra" virtual service to the appropriate world based on role
	if serviceID == "spectra" {
		role := strings.ToLower(strings.TrimSpace(req.Role))
		var world string
		switch role {
		case "source":
			world = "primary"
		case "destination":
			world = "s1"
		default:
			// Default to primary if no role specified
			world = "primary"
		}

		def, err := m.findSpectraServiceByWorld(world)
		if err != nil {
			return types.ListResult{}, types.PaginationInfo{}, fmt.Errorf("spectra service with world %s not found: %w", world, err)
		}

		result, err := m.listSpectraChildren(def, req.Identifier)
		if err != nil {
			return types.ListResult{}, types.PaginationInfo{}, err
		}

		// Spectra doesn't need pagination for now, but apply it anyway for consistency
		paginated, pagination := applyPagination(result, req.Offset, req.Limit, req.FoldersOnly)
		return paginated, pagination, nil
	}

	// Map "local" virtual service to the first available local service
	// Since there's only one filesystem per machine, this makes sense
	// For "local" virtual service, allow unrestricted browsing (ignore rootPath restrictions)
	if serviceID == "local" {
		m.mu.RLock()
		var localDef serviceDefinition
		found := false
		for _, svc := range m.services {
			if svc.Type == types.ServiceTypeLocal {
				localDef = svc
				found = true
				break
			}
		}
		m.mu.RUnlock()

		if !found {
			return types.ListResult{}, types.PaginationInfo{}, fmt.Errorf("no local filesystem services configured")
		}

		// Create a temporary service def without rootPath restriction for unrestricted browsing
		unrestrictedDef := localDef
		if unrestrictedDef.Local != nil {
			// Create a copy with empty rootPath to allow unrestricted browsing
			unrestrictedLocal := *unrestrictedDef.Local
			unrestrictedLocal.RootPath = "" // Empty rootPath = unrestricted
			unrestrictedDef.Local = &unrestrictedLocal
		}

		result, err := m.listLocalChildren(unrestrictedDef, req.Identifier, req.Offset, req.Limit, req.FoldersOnly)
		if err != nil {
			return types.ListResult{}, types.PaginationInfo{}, err
		}

		return result.Result, result.Pagination, nil
	}

	def, err := m.serviceDefinition(serviceID)
	if err != nil {
		return types.ListResult{}, types.PaginationInfo{}, err
	}

	switch def.Type {
	case types.ServiceTypeLocal:
		result, err := m.listLocalChildren(def, req.Identifier, req.Offset, req.Limit, req.FoldersOnly)
		if err != nil {
			return types.ListResult{}, types.PaginationInfo{}, err
		}
		return result.Result, result.Pagination, nil

	case types.ServiceTypeSpectra:
		result, err := m.listSpectraChildren(def, req.Identifier)
		if err != nil {
			return types.ListResult{}, types.PaginationInfo{}, err
		}
		// Spectra doesn't need pagination for now, but apply it anyway for consistency
		paginated, pagination := applyPagination(result, req.Offset, req.Limit, req.FoldersOnly)
		return paginated, pagination, nil

	default:
		return types.ListResult{}, types.PaginationInfo{}, fmt.Errorf("unsupported service type: %s", def.Type)
	}
}

func (m *ServiceManager) listLocalChildren(def serviceDefinition, identifier string, offset, limit int, foldersOnly bool) (paginatedListResult, error) {
	if def.Local == nil {
		return paginatedListResult{}, fmt.Errorf("local service %s missing configuration", def.ID)
	}

	root := def.Local.RootPath
	var adapter *LocalFS
	var err error

	// If rootPath is empty, allow unrestricted browsing
	// Create adapter based on the target path's root (drive root on Windows, system root on Unix)
	if root == "" {
		// Use the identifier as the target, or default to system root
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
			return paginatedListResult{}, fmt.Errorf("failed to resolve path: %w", err)
		}
		cleanTarget = filepath.Clean(cleanTarget)

		// Determine the adapter root based on the target path
		// On Windows, use the drive root (e.g., C:\) as the adapter root
		// On Unix, use system root (/)
		var adapterRoot string
		if runtime.GOOS == "windows" {
			drive := filepath.VolumeName(cleanTarget)
			if drive != "" {
				adapterRoot = drive + "\\"
			} else {
				adapterRoot = "C:\\" // Fallback
			}
		} else {
			adapterRoot = "/"
		}

		adapter, err = NewLocalFS(adapterRoot)
		if err != nil {
			return paginatedListResult{}, err
		}

		result, err := adapter.ListChildren(cleanTarget)
		if err != nil {
			return paginatedListResult{}, err
		}

		// Apply pagination
		paginated, pagination := applyPagination(result, offset, limit, foldersOnly)
		return paginatedListResult{Result: paginated, Pagination: pagination}, nil
	}

	// Restricted browsing within rootPath
	adapter, err = NewLocalFS(root)
	if err != nil {
		return paginatedListResult{}, err
	}

	target := identifier
	if target == "" {
		target = root
	}

	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return paginatedListResult{}, err
	}
	cleanTarget = filepath.Clean(cleanTarget)

	if !hasPathPrefix(cleanTarget, root) {
		return paginatedListResult{}, fmt.Errorf("path %s is outside allowed root %s", cleanTarget, root)
	}

	result, err := adapter.ListChildren(cleanTarget)
	if err != nil {
		return paginatedListResult{}, err
	}

	// Apply pagination
	paginated, pagination := applyPagination(result, offset, limit, foldersOnly)
	return paginatedListResult{Result: paginated, Pagination: pagination}, nil
}

func (m *ServiceManager) listSpectraChildren(def serviceDefinition, identifier string) (types.ListResult, error) {
	if def.Spectra == nil {
		return types.ListResult{}, fmt.Errorf("spectra service %s missing configuration", def.ID)
	}

	spectraFS, err := sdk.New(def.Spectra.ConfigPath)
	if err != nil {
		return types.ListResult{}, err
	}
	defer spectraFS.Close()

	root := def.Spectra.RootID
	if root == "" {
		root = "root"
	}

	adapter, err := NewSpectraFS(spectraFS, root, def.Spectra.World)
	if err != nil {
		return types.ListResult{}, err
	}

	target := identifier
	if target == "" {
		target = root
	}

	return adapter.ListChildren(target)
}

// ListDrives lists available drives/volumes on the system
// This is primarily useful for Windows (C:\, D:\, etc.) but also works on Unix systems
// serviceID can be "local" (virtual service) or a specific local service ID
func (m *ServiceManager) ListDrives(ctx context.Context, serviceID string) ([]types.DriveInfo, error) {
	// Handle "local" as a virtual service ID (similar to "spectra")
	// Drives are system-wide, not service-specific, so we just need to verify
	// that local services exist
	if serviceID == "local" {
		m.mu.RLock()
		hasLocal := false
		for _, svc := range m.services {
			if svc.Type == types.ServiceTypeLocal {
				hasLocal = true
				break
			}
		}
		m.mu.RUnlock()

		if !hasLocal {
			return nil, fmt.Errorf("no local filesystem services configured")
		}
		// Proceed with drive listing - drives are the same regardless of which local service
	} else {
		def, err := m.serviceDefinition(serviceID)
		if err != nil {
			return nil, err
		}
		if def.Type != types.ServiceTypeLocal {
			return nil, fmt.Errorf("list drives is only supported for local filesystem services")
		}
	}

	var drives []types.DriveInfo

	if runtime.GOOS == "windows" {
		// On Windows, enumerate drive letters from A to Z
		for letter := 'A'; letter <= 'Z'; letter++ {
			drivePath := string(letter) + ":\\"
			// Check if drive exists by trying to get its info
			info, err := os.Stat(drivePath)
			if err != nil {
				continue // Drive doesn't exist or isn't accessible
			}
			if !info.IsDir() {
				continue
			}

			// Try to determine drive type by checking if we can read it
			driveType := "unknown"
			_, err = os.ReadDir(drivePath)
			if err == nil {
				// Basic heuristic: if it's accessible, try to determine type
				// On Windows, we could use GetDriveType API, but for now just mark as accessible
				driveType = "fixed" // Default assumption
			}

			drives = append(drives, types.DriveInfo{
				Path:        drivePath,
				DisplayName: string(letter),
				Type:        driveType,
			})
		}
	} else {
		// On Unix-like systems, list common mount points
		// Start with root
		drives = append(drives, types.DriveInfo{
			Path:        "/",
			DisplayName: "Root",
			Type:        "filesystem",
		})

		// Try to list common mount points
		mountPoints := []string{"/mnt", "/media", "/Volumes"}
		for _, mountPoint := range mountPoints {
			entries, err := os.ReadDir(mountPoint)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					fullPath := filepath.Join(mountPoint, entry.Name())
					drives = append(drives, types.DriveInfo{
						Path:        fullPath,
						DisplayName: entry.Name(),
						Type:        "mount",
					})
				}
			}
		}
	}

	return drives, nil
}

func (m *ServiceManager) serviceDefinition(id string) (serviceDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	def, ok := m.services[id]
	if !ok {
		return serviceDefinition{}, ErrServiceNotFound
	}
	return def, nil
}

// findSpectraServiceByWorld finds the first Spectra service with the given world.
func (m *ServiceManager) findSpectraServiceByWorld(world string) (serviceDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, svc := range m.services {
		if svc.Type == types.ServiceTypeSpectra && svc.Spectra != nil && svc.Spectra.World == world {
			return svc, nil
		}
	}
	return serviceDefinition{}, ErrServiceNotFound
}

// GetServiceDefinitionByWorld finds the first Spectra service with the given world.
func (m *ServiceManager) GetServiceDefinitionByWorld(world string) (types.ServiceDefinition, error) {
	return m.findSpectraServiceByWorld(world)
}

// AcquireAdapter acquires an adapter for the given service definition
func (m *ServiceManager) AcquireAdapter(def serviceDefinition, rootID, connectionID string) (types.FSAdapter, func(), error) {
	return m.AcquireAdapterWithOverride(def, rootID, connectionID, "")
}

// AcquireAdapterWithOverride acquires an adapter, with optional Spectra config override path
func (m *ServiceManager) AcquireAdapterWithOverride(def serviceDefinition, rootID, connectionID, spectraConfigOverridePath string) (types.FSAdapter, func(), error) {
	switch def.Type {
	case types.ServiceTypeLocal:
		if rootID == "" {
			return nil, nil, fmt.Errorf("local root path cannot be empty")
		}

		adapter, err := NewLocalFS(rootID)
		if err != nil {
			return nil, nil, err
		}

		return adapter, func() {}, nil

	case types.ServiceTypeSpectra:
		if def.Spectra == nil {
			return nil, nil, fmt.Errorf("spectra configuration missing")
		}

		return m.acquireSpectraAdapter(def, rootID, connectionID, spectraConfigOverridePath)

	default:
		return nil, nil, fmt.Errorf("unsupported service type: %s", def.Type)
	}
}

func (m *ServiceManager) acquireSpectraAdapter(def serviceDefinition, rootID, connectionID, spectraConfigOverridePath string) (types.FSAdapter, func(), error) {
	root := def.Spectra.RootID
	if rootID != "" {
		root = rootID
	}

	// Use override config path if provided, otherwise use original
	configPath := def.Spectra.ConfigPath
	if spectraConfigOverridePath != "" {
		configPath = spectraConfigOverridePath
	}

	if connectionID == "" {
		spectraFS, err := sdk.New(configPath)
		if err != nil {
			return nil, nil, err
		}

		adapter, err := NewSpectraFS(spectraFS, root, def.Spectra.World)
		if err != nil {
			_ = spectraFS.Close()
			return nil, nil, err
		}

		return adapter, func() {
			_ = spectraFS.Close()
		}, nil
	}

	m.mu.Lock()
	conn, ok := m.connections[connectionID]
	if ok {
		if conn.typ != types.ServiceTypeSpectra {
			m.mu.Unlock()
			return nil, nil, fmt.Errorf("connection %s is already in use by a different service type", connectionID)
		}

		conn.refCount++
		spectraFS := conn.spectra
		m.mu.Unlock()

		adapter, err := NewSpectraFS(spectraFS, root, def.Spectra.World)
		if err != nil {
			m.ReleaseConnection(connectionID)
			return nil, nil, err
		}

		return adapter, func() {
			m.ReleaseConnection(connectionID)
		}, nil
	}
	m.mu.Unlock()

	spectraFS, err := sdk.New(configPath)
	if err != nil {
		return nil, nil, err
	}

	adapter, err := NewSpectraFS(spectraFS, root, def.Spectra.World)
	if err != nil {
		_ = spectraFS.Close()
		return nil, nil, err
	}

	m.mu.Lock()
	if existing, exists := m.connections[connectionID]; exists {
		existing.refCount++
		spectraShared := existing.spectra
		m.mu.Unlock()

		_ = spectraFS.Close()

		adapter, err := NewSpectraFS(spectraShared, root, def.Spectra.World)
		if err != nil {
			m.ReleaseConnection(connectionID)
			return nil, nil, err
		}

		return adapter, func() {
			m.ReleaseConnection(connectionID)
		}, nil
	}

	m.connections[connectionID] = &serviceConnection{
		typ:      types.ServiceTypeSpectra,
		spectra:  spectraFS,
		refCount: 1,
	}
	m.mu.Unlock()

	return adapter, func() {
		m.ReleaseConnection(connectionID)
	}, nil
}

// ReleaseConnection releases a connection reference
func (m *ServiceManager) ReleaseConnection(connectionID string) {
	if connectionID == "" {
		return
	}

	m.mu.Lock()
	conn, ok := m.connections[connectionID]
	if !ok {
		m.mu.Unlock()
		return
	}

	conn.refCount--
	if conn.refCount <= 0 {
		delete(m.connections, connectionID)
		switch conn.typ {
		case types.ServiceTypeSpectra:
			if conn.spectra != nil {
				_ = conn.spectra.Close()
			}
		}
	}
	m.mu.Unlock()
}

// GetServiceDefinition returns a service definition by ID
func (m *ServiceManager) GetServiceDefinition(id string) (types.ServiceDefinition, error) {
	return m.serviceDefinition(id)
}

func hasPathPrefix(path, root string) bool {
	if path == root {
		return true
	}
	sep := string(filepath.Separator)
	if strings.HasPrefix(path, root+sep) {
		return true
	}
	if sep != "/" && strings.HasPrefix(path, root+"/") {
		return true
	}
	return false
}

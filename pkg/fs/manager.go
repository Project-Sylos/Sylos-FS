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
	session  *SpectraSession // For Spectra services, use session instead of raw SDK
	refCount int
}

// NewServiceManager creates a new ServiceManager instance
func NewServiceManager() *ServiceManager {
	return &ServiceManager{
		services:    make(map[string]serviceDefinition),
		connections: make(map[string]*serviceConnection),
	}
}

// ============================================================================
// Service getters/setters (with locking)
// ============================================================================

// getService returns a service definition by ID (read lock)
func (m *ServiceManager) getService(id string) (serviceDefinition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	def, exists := m.services[id]
	return def, exists
}

// setService sets a service definition (write lock)
func (m *ServiceManager) setService(id string, def serviceDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[id] = def
}

// getAllServices returns a copy of all services (read lock)
func (m *ServiceManager) getAllServices() map[string]serviceDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]serviceDefinition, len(m.services))
	for k, v := range m.services {
		result[k] = v
	}
	return result
}

// findLocalService returns the first local service found (read lock)
func (m *ServiceManager) findLocalService() (serviceDefinition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, svc := range m.services {
		if svc.Type == types.ServiceTypeLocal {
			return svc, true
		}
	}
	return serviceDefinition{}, false
}

// hasLocalService checks if any local service exists (read lock)
func (m *ServiceManager) hasLocalService() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, svc := range m.services {
		if svc.Type == types.ServiceTypeLocal {
			return true
		}
	}
	return false
}

// findSpectraServiceByWorld finds the first Spectra service with the given world (read lock)
func (m *ServiceManager) findSpectraServiceByWorldLocked(world string) (serviceDefinition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, svc := range m.services {
		if svc.Type == types.ServiceTypeSpectra && svc.Spectra != nil && svc.Spectra.World == world {
			return svc, true
		}
	}
	return serviceDefinition{}, false
}

// ============================================================================
// Connection getters/setters (with locking)
// ============================================================================

// getConnection returns a connection by ID (read lock)
func (m *ServiceManager) getConnection(connectionID string) (*serviceConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, exists := m.connections[connectionID]
	return conn, exists
}

// registerConnection registers a connection (write lock)
func (m *ServiceManager) registerConnection(connectionID string, conn *serviceConnection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.connections[connectionID]; exists {
		return fmt.Errorf("connection %s already exists", connectionID)
	}
	m.connections[connectionID] = conn
	return nil
}

// incrementConnectionRefCount increments the reference count for a connection (write lock)
func (m *ServiceManager) incrementConnectionRefCount(connectionID string) (*serviceConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, exists := m.connections[connectionID]
	if !exists {
		return nil, fmt.Errorf("connection %s not found", connectionID)
	}
	conn.refCount++
	return conn, nil
}

// decrementConnectionRefCount decrements the reference count and returns whether it should be deleted (write lock)
func (m *ServiceManager) decrementConnectionRefCount(connectionID string) (*serviceConnection, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, exists := m.connections[connectionID]
	if !exists {
		return nil, false
	}
	conn.refCount--
	shouldDelete := conn.refCount <= 0
	if shouldDelete {
		delete(m.connections, connectionID)
	}
	return conn, shouldDelete
}

// LoadServices loads services from the provided service definitions
func (m *ServiceManager) LoadServices(localServices []types.LocalServiceConfig, spectraServices []types.SpectraServiceConfig) error {
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

		m.setService(def.ID, def)
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

		m.setService(def.ID, def)
	}

	return nil
}

// ListSources returns a list of all available sources
func (m *ServiceManager) ListSources(ctx context.Context) ([]types.Source, error) {
	sources := make([]types.Source, 0)
	hasSpectra := false
	var spectraConfigPath string

	services := m.getAllServices()
	for _, svc := range services {
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
func applyPagination(result types.ListResult, offset, limit int,
	foldersOnly bool) (types.ListResult, types.PaginationInfo) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	total := len(result.Folders) + len(result.Files)
	totalFolders := len(result.Folders)
	totalFiles := len(result.Files)

	var paginatedResult types.ListResult

	// Apply foldersOnly filter if requested
	if foldersOnly {
		paginatedResult.Folders = result.Folders
		paginatedResult.Files = []types.File{} // Empty files when foldersOnly is true
	} else {
		paginatedResult.Folders = result.Folders
		paginatedResult.Files = result.Files
	}

	// Apply offset and limit to folders
	if offset < len(paginatedResult.Folders) {
		end := offset + limit
		if end > len(paginatedResult.Folders) {
			end = len(paginatedResult.Folders)
		}
		paginatedResult.Folders = paginatedResult.Folders[offset:end]
	} else {
		paginatedResult.Folders = []types.Folder{}
	}

	// If foldersOnly is false, also apply offset and limit to files
	if !foldersOnly {
		// Adjust file offset based on how many folders we skipped
		fileOffset := offset
		if fileOffset > totalFolders {
			fileOffset = fileOffset - totalFolders
		} else {
			fileOffset = 0
		}

		// Adjust file limit based on how many folders we already included
		fileLimit := limit - len(paginatedResult.Folders)
		if fileLimit < 0 {
			fileLimit = 0
		}

		if fileOffset < len(paginatedResult.Files) {
			end := fileOffset + fileLimit
			if end > len(paginatedResult.Files) {
				end = len(paginatedResult.Files)
			}
			paginatedResult.Files = paginatedResult.Files[fileOffset:end]
		} else {
			paginatedResult.Files = []types.File{}
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

		// ListChildren for Spectra now requires a session ID
		if req.SessionID == "" {
			return types.ListResult{}, types.PaginationInfo{}, fmt.Errorf("listing Spectra children requires a session ID")
		}
		result, err := m.listSpectraChildren(def, req.Identifier, req.SessionID)
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
		localDef, found := m.findLocalService()

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
		// ListChildren for Spectra now requires a session ID
		if req.SessionID == "" {
			return types.ListResult{}, types.PaginationInfo{}, fmt.Errorf("listing Spectra children requires a session ID")
		}
		result, err := m.listSpectraChildren(def, req.Identifier, req.SessionID)
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

// listSpectraChildren lists children using a session. This function requires a session ID.
// It does NOT create or close Spectra - it must be given a valid session.
func (m *ServiceManager) listSpectraChildren(def serviceDefinition, identifier string, sessionID string) (types.ListResult, error) {
	if def.Spectra == nil {
		return types.ListResult{}, fmt.Errorf("spectra service %s missing configuration", def.ID)
	}

	if sessionID == "" {
		return types.ListResult{}, fmt.Errorf("listSpectraChildren requires a session ID - no session provided")
	}

	// Get the session from the connection pool
	conn, exists := m.getConnection(sessionID)

	if !exists {
		return types.ListResult{}, fmt.Errorf("session %s not found - cannot list children without an active session", sessionID)
	}

	if conn.typ != types.ServiceTypeSpectra {
		return types.ListResult{}, fmt.Errorf("session %s is not a Spectra session", sessionID)
	}

	if conn.session == nil {
		return types.ListResult{}, fmt.Errorf("session %s has no Spectra session instance", sessionID)
	}

	if conn.session.IsClosed() {
		return types.ListResult{}, fmt.Errorf("session %s is closed", sessionID)
	}

	root := def.Spectra.RootID
	if root == "" {
		root = "root"
	}

	adapter, err := conn.session.CreateAdapter(root, def.Spectra.World)
	if err != nil {
		return types.ListResult{}, fmt.Errorf("failed to create adapter from session: %w", err)
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
	// that a local service exists (we don't need to use it)
	if serviceID == "local" {
		if !m.hasLocalService() {
			return nil, fmt.Errorf("no local filesystem services configured")
		}
	} else {
		// Verify the service exists
		_, err := m.serviceDefinition(serviceID)
		if err != nil {
			return nil, err
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
				// Drive is accessible - could be fixed, removable, or network
				// Windows API would be needed for precise type, but this is a reasonable approximation
				driveType = "fixed" // Default assumption
			}

			drives = append(drives, types.DriveInfo{
				Path: drivePath,
				Type: driveType,
			})
		}
	} else {
		// On Unix systems, just return the root directory as a single "drive"
		drives = append(drives, types.DriveInfo{
			Path: "/",
			Type: "fixed",
		})
	}

	return drives, nil
}

// serviceDefinition returns a service definition by ID
func (m *ServiceManager) serviceDefinition(id string) (serviceDefinition, error) {
	def, exists := m.getService(id)
	if !exists {
		return serviceDefinition{}, ErrServiceNotFound
	}
	return def, nil
}

// findSpectraServiceByWorld finds the first Spectra service with the given world.
func (m *ServiceManager) findSpectraServiceByWorld(world string) (serviceDefinition, error) {
	def, exists := m.findSpectraServiceByWorldLocked(world)
	if !exists {
		return serviceDefinition{}, ErrServiceNotFound
	}
	return def, nil
}

// GetServiceDefinitionByWorld finds the first Spectra service with the given world.
func (m *ServiceManager) GetServiceDefinitionByWorld(world string) (types.ServiceDefinition, error) {
	return m.findSpectraServiceByWorld(world)
}

// RegisterSpectraSession creates a new Spectra session and registers it with the ServiceManager.
// The ServiceManager owns the session lifecycle - the API should not hold a reference to it.
// Returns a sessionID that can be used to acquire adapters from this session.
// If connectionID is empty, a unique ID will be generated.
func (m *ServiceManager) RegisterSpectraSession(configPath string, connectionID string) (string, error) {
	if configPath == "" {
		return "", fmt.Errorf("configPath cannot be empty")
	}

	// Generate connectionID if not provided
	if connectionID == "" {
		// Generate a unique ID (simple approach)
		m.mu.RLock()
		baseID := len(m.connections)
		m.mu.RUnlock()

		connectionID = fmt.Sprintf("spectra-%d", baseID)
		// Ensure uniqueness
		for {
			if _, exists := m.getConnection(connectionID); !exists {
				break
			}
			baseID++
			connectionID = fmt.Sprintf("spectra-%d", baseID)
		}
	}

	// Create the session (this calls sdk.New() internally)
	session, err := NewSpectraSession(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to create Spectra session: %w", err)
	}

	// Register the connection
	conn := &serviceConnection{
		typ:      types.ServiceTypeSpectra,
		session:  session,
		refCount: 0, // Will be incremented when adapters are acquired
	}

	if err := m.registerConnection(connectionID, conn); err != nil {
		// Registration failed - close the session we created
		_ = session.Close()
		return "", err
	}

	return connectionID, nil
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

		return m.acquireSpectraAdapter(def, rootID, connectionID)

	default:
		return nil, nil, fmt.Errorf("unsupported service type: %s", def.Type)
	}
}

func (m *ServiceManager) acquireSpectraAdapter(def serviceDefinition, rootID, connectionID string) (types.FSAdapter, func(), error) {
	root := def.Spectra.RootID
	if rootID != "" {
		root = rootID
	}

	// connectionID is required for Spectra - session must be registered first
	if connectionID == "" {
		return nil, nil, fmt.Errorf("connectionID is required for Spectra services - session must be registered first using RegisterSpectraSession")
	}

	// Get existing connection (session must be registered by caller)
	conn, exists := m.getConnection(connectionID)
	if !exists {
		return nil, nil, fmt.Errorf("connection %s not found - session must be registered first using RegisterSpectraSession", connectionID)
	}

	// Validate connection type
	if conn.typ != types.ServiceTypeSpectra {
		return nil, nil, fmt.Errorf("connection %s is not a Spectra connection", connectionID)
	}

	if conn.session == nil {
		return nil, nil, fmt.Errorf("connection %s has no session", connectionID)
	}

	if conn.session.IsClosed() {
		return nil, nil, fmt.Errorf("connection %s session is closed", connectionID)
	}

	// Increment ref count
	_, err := m.incrementConnectionRefCount(connectionID)
	if err != nil {
		return nil, nil, err
	}

	// Create adapter from the registered session
	adapter, err := conn.session.CreateAdapter(root, def.Spectra.World)
	if err != nil {
		m.ReleaseConnection(connectionID)
		return nil, nil, fmt.Errorf("failed to create adapter from session: %w", err)
	}

	return adapter, func() {
		m.ReleaseConnection(connectionID)
	}, nil
}

// ReleaseConnection releases a connection reference
func (m *ServiceManager) ReleaseConnection(connectionID string) {
	if connectionID == "" {
		return
	}

	conn, shouldDelete := m.decrementConnectionRefCount(connectionID)
	if !shouldDelete {
		return
	}

	// Connection should be deleted - close it if it's a Spectra session
	if conn != nil {
		switch conn.typ {
		case types.ServiceTypeSpectra:
			if conn.session != nil {
				_ = conn.session.Close() // Close the session
			}
		}
	}
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

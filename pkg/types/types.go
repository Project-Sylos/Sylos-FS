// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
	"context"
	"io"
	"path"
	"strings"
)

const (
	NodeTypeFile   = "file"
	NodeTypeFolder = "folder"
)

// ServiceType represents the type of service
type ServiceType string

const (
	ServiceTypeLocal   ServiceType = "local"
	ServiceTypeSpectra ServiceType = "spectra"
	ServiceTypeCloud   ServiceType = "cloud"
)

// Folder represents a basic folder with attributes like name, path, identifier, and parentID.
type Folder struct {
	ServiceID    string
	ParentId     string
	ParentPath   string // The parent's relative path (root-relative)
	DisplayName  string
	LocationPath string
	LastUpdated  string
	DepthLevel   int
	Type         string // "folder"
}

func (f Folder) ID() string       { return f.ServiceID }
func (f Folder) Name() string     { return f.DisplayName }
func (f Folder) Path() string     { return f.LocationPath }
func (f Folder) ParentID() string { return f.ParentId }
func (f Folder) NodeType() string { return f.Type }

// File represents a basic file with attributes like name, path, identifier, and parentID.
type File struct {
	ServiceID    string
	ParentId     string
	ParentPath   string // The parent's relative path (root-relative)
	DisplayName  string
	LocationPath string
	LastUpdated  string
	DepthLevel   int
	Size         int64
	Type         string // "file"
}

func (f File) ID() string       { return f.ServiceID }
func (f File) Name() string     { return f.DisplayName }
func (f File) Path() string     { return f.LocationPath }
func (f File) ParentID() string { return f.ParentId }
func (f File) NodeType() string { return f.Type }

// Node is a common interface for both files and folders
type Node interface {
	ID() string
	Name() string
	Path() string
	ParentID() string
	NodeType() string
}

// ListResult contains the folders and files from a ListChildren operation
type ListResult struct {
	Folders []Folder
	Files   []File
}

// ListPage represents a single "page" of children returned from a paginated listing.
// It includes a Total count so callers can understand how many children exist in
// aggregate, even if only a subset is present in this page.
type ListPage struct {
	Folders  []Folder
	Files    []File
	Total    int // Total number of children (folders + files) across all pages
	Page     int // 0-based page index
	PageSize int // Requested maximum number of children per page
	HasMore  bool
}

// ListPager provides a simple in-memory pagination wrapper around a full ListResult.
// This allows callers (like traversal workers) to process children in fixed-size pages
// even when the underlying filesystem adapter doesn't support pagination natively.
//
// For cloud services that do support pagination, a future adapter-specific pager can
// be implemented that fetches one page at a time from the remote API. For now, this
// implementation virtualizes pagination using a single ListChildren call and array
// slicing, which is sufficient for local filesystems and small to medium directory sizes.
type ListPager struct {
	allFolders []Folder
	allFiles   []File
	total      int
	pageSize   int
	page       int
	index      int // linear index across folders+files
}

// NewListPager constructs a ListPager over an existing ListResult, using the provided
// pageSize as the maximum number of children (folders+files) per page.
func NewListPager(result ListResult, pageSize int) *ListPager {
	if pageSize <= 0 {
		pageSize = 100
	}
	total := len(result.Folders) + len(result.Files)
	return &ListPager{
		allFolders: result.Folders,
		allFiles:   result.Files,
		total:      total,
		pageSize:   pageSize,
		page:       0,
		index:      0,
	}
}

// Next returns the next page of children and a boolean indicating whether any
// page was returned. When no more pages remain, hasPage will be false.
func (p *ListPager) Next() (page ListPage, hasPage bool) {
	if p == nil || p.index >= p.total {
		return ListPage{}, false
	}

	remaining := p.total - p.index
	count := p.pageSize
	if remaining < count {
		count = remaining
	}

	// Build this page by walking a linear index across folders then files.
	folders := make([]Folder, 0, count)
	files := make([]File, 0, count)
	for i := 0; i < count; i++ {
		globalIdx := p.index + i
		if globalIdx < len(p.allFolders) {
			folders = append(folders, p.allFolders[globalIdx])
		} else {
			fileIdx := globalIdx - len(p.allFolders)
			if fileIdx < len(p.allFiles) {
				files = append(files, p.allFiles[fileIdx])
			}
		}
	}

	p.index += count
	currentPage := p.page
	p.page++

	return ListPage{
		Folders:  folders,
		Files:    files,
		Total:    p.total,
		Page:     currentPage,
		PageSize: p.pageSize,
		HasMore:  p.index < p.total,
	}, true
}

// FSAdapter is the interface that all filesystem adapters must implement
type FSAdapter interface {
	ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (ListResult, error)
	OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error)
	CreateFolder(ctx context.Context, parentId, name string, metadata map[string]string) (Folder, error)
	CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (File, error)
	OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error)
	DeleteNode(ctx context.Context, nodeID string, nodeType string) error
	NormalizePath(path string) string

	// Initialize configures the adapter with the envelope master key and stable
	// connectionID. Cloud adapters derive a per-connection key (HKDF) to decrypt
	// creds.conf; local and Spectra implementations are no-ops.
	Initialize(masterKey []byte, connectionID string) error

	// RegisterCredentials stores new credentials (e.g. from OAuth), encrypted
	// with a key derived from masterKey and connectionID. Local and Spectra are no-ops.
	RegisterCredentials(credsData []byte, masterKey []byte, connectionID string) error

	// HasValidCredentials reports whether the adapter has usable credentials.
	// Local and Spectra implementations return true.
	HasValidCredentials() bool
}

// ServiceContext wraps a filesystem adapter with a service name
type ServiceContext struct {
	Name      string    // 'Windows', 'Dropbox', 'Google Drive', 'Spectra', etc.
	Connector FSAdapter // The pointer to the actual instance of the service adapter
}

// NewServiceContext creates a new ServiceContext with the given name and adapter.
// This is the universal way to create service contexts for any filesystem type.
func NewServiceContext(name string, adapter FSAdapter) *ServiceContext {
	return &ServiceContext{
		Name:      name,
		Connector: adapter,
	}
}

// NormalizeLocationPath ensures logical paths always use forward slashes, are rooted,
// and collapse redundant separators. Empty paths normalize to "/".
func NormalizeLocationPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

// ListChildrenBasePath picks the migration root-relative parent for child LocationPath construction.
// Prefer the caller-supplied parentPath (current folder being listed); fall back to migration root.
func ListChildrenBasePath(rootLocationPath, parentPath string) string {
	if pp := strings.TrimSpace(parentPath); pp != "" {
		return NormalizeLocationPath(pp)
	}
	return NormalizeLocationPath(rootLocationPath)
}

// LogicalParentFromCreateMetadata returns the migration root-relative parent path for create
// responses when metadata carries location_path or parent_path (copy/ME convention).
func LogicalParentFromCreateMetadata(metadata map[string]string, fallback string) string {
	if metadata != nil {
		if loc := strings.TrimSpace(metadata["location_path"]); loc != "" {
			loc = NormalizeLocationPath(loc)
			dir := path.Dir(loc)
			if dir == "." || dir == "" {
				return "/"
			}
			return NormalizeLocationPath(dir)
		}
		if pp := strings.TrimSpace(metadata["parent_path"]); pp != "" {
			return NormalizeLocationPath(pp)
		}
	}
	return NormalizeLocationPath(fallback)
}

// ChildLocationFromCreateMetadata returns the migration root-relative child path for create
// responses when metadata carries location_path; otherwise joins parentPath and name.
func ChildLocationFromCreateMetadata(metadata map[string]string, parentPath, name string) string {
	if metadata != nil {
		if loc := strings.TrimSpace(metadata["location_path"]); loc != "" {
			return NormalizeLocationPath(loc)
		}
	}
	pp := NormalizeLocationPath(parentPath)
	if pp == "/" {
		return NormalizeLocationPath("/" + name)
	}
	return NormalizeLocationPath(pp + "/" + name)
}

// NormalizeParentPath normalizes stored parent_path strings but preserves empty values
// (used by root nodes which have no parent).
func NormalizeParentPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return NormalizeLocationPath(p)
}

// Source represents a source service
type Source struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"displayName"`
	Type        ServiceType       `json:"type"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// NodeRef identifies a file or folder for batch operations.
type NodeRef struct {
	ID   string
	Type string // NodeTypeFile or NodeTypeFolder
}

// DeleteNodeError describes a single failed delete in a batch.
type DeleteNodeError struct {
	ID      string
	Message string
}

// DeleteNodesResult is the outcome of a batch delete (partial success allowed).
type DeleteNodesResult struct {
	Deleted []string
	Errors  []DeleteNodeError
}

// BrowseMutationRequest scopes create/delete operations during folder browsing.
type BrowseMutationRequest struct {
	ServiceID    string
	ConnectionID string // cloud / spectra session
	Role         string // source | destination (spectra world mapping)
	RootType     string
	DriveID      string
	ContextID    string // current browse folder id (cloud adapter context)
}

// ListChildrenRequest represents a request to list children
type ListChildrenRequest struct {
	ServiceID   string
	Identifier  string
	Role        string // "source" or "destination" - used to map "spectra" to the correct world
	SessionID   string // Session ID for Spectra services (required for Spectra, ignored for others)
	RootType    string // Cloud browse root type (my_drive, user_root, team_folder, etc.)
	DriveID     string // Cloud namespace metadata (Dropbox team_folder/shared_folder)
	Offset      int    // Pagination offset (default: 0)
	Limit       int    // Pagination limit (default: 100, max: 1000)
	FoldersOnly bool   // If true, only return folders and apply limit to folders only
	Depth       *int   // Depth level to list children at (required for ephemeral mode, optional for persistent mode)
	ParentPath  string // Parent path (required for Spectra ephemeral mode; caller must supply, e.g. "/" for root)
}

// PaginationInfo provides pagination metadata
type PaginationInfo struct {
	Offset       int  `json:"offset"`
	Limit        int  `json:"limit"`
	Total        int  `json:"total"`
	TotalFolders int  `json:"totalFolders"`
	TotalFiles   int  `json:"totalFiles"`
	HasMore      bool `json:"hasMore"`
}

// DriveInfo represents information about a drive/volume
type DriveInfo struct {
	Path        string `json:"path"`                  // Browse root: mount point or block device path
	DisplayName string `json:"displayName,omitempty"` // Human-readable label for UI defaults
	Type        string `json:"type"`                  // fixed, removable, network, unknown
	MountPoint  string `json:"mountPoint,omitempty"`  // Mount path when mounted (same as Path for mounted volumes)
	Device      string `json:"device,omitempty"`      // Block device path (e.g. /dev/sda2, \\.\PhysicalDrive0)
	FileSystem  string `json:"fileSystem,omitempty"`  // ext4, ntfs, apfs, etc.
	Mounted     bool   `json:"mounted"`               // Whether the volume is currently mounted
	Label       string `json:"label,omitempty"`       // Volume label when available
	TotalBytes  int64  `json:"totalBytes"`            // Total capacity in bytes; 0 if unknown
	FreeBytes   int64  `json:"freeBytes"`             // Available space in bytes; 0 if unknown
	UsedBytes   int64  `json:"usedBytes"`             // Used space in bytes; 0 if unknown
}

// LocalServiceConfig represents configuration for a local filesystem service
type LocalServiceConfig struct {
	ID       string
	Name     string
	RootPath string // Empty means unrestricted browsing
}

// SpectraSessionOptions are optional overrides when creating a Spectra session.
// Nil values mean use the config file as-is.
type SpectraSessionOptions struct {
	// DivergingTreeMode (ephemeral only): if non-nil, overrides seed.diverging_tree_mode in config.
	// When true, each world gets a different tree shape (e.g. for copy tests).
	// When false or nil (default), same path in any world yields same children.
	DivergingTreeMode *bool
}

// SpectraServiceConfig represents configuration for a Spectra filesystem service
type SpectraServiceConfig struct {
	ID         string
	Name       string
	World      string // "primary", "s1", "s2", etc.
	RootID     string // Typically "root"
	ConfigPath string // Path to Spectra config file
	// DivergingTreeMode: if non-nil, use when creating a session for this service (ephemeral only).
	DivergingTreeMode *bool
}

// CloudServiceConfig represents configuration for a cloud filesystem provider entry.
type CloudServiceConfig struct {
	ID         string
	Name       string
	ProviderID string // google_drive, dropbox, etc.
}

// ServiceDefinition represents a service definition
type ServiceDefinition struct {
	ID      string
	Name    string
	Type    ServiceType
	Local   *LocalServiceConfig
	Spectra *SpectraServiceConfig
	Cloud   *CloudServiceConfig
}

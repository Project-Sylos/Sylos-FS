// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
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
)

// Folder represents a basic folder with attributes like name, path, identifier, and parentID.
type Folder struct {
	Id           string
	ParentId     string
	ParentPath   string // The parent's relative path (root-relative)
	DisplayName  string
	LocationPath string
	LastUpdated  string
	DepthLevel   int
	Type         string // "folder"
}

func (f Folder) ID() string       { return f.Id }
func (f Folder) Name() string     { return f.DisplayName }
func (f Folder) Path() string     { return f.LocationPath }
func (f Folder) ParentID() string { return f.ParentId }
func (f Folder) NodeType() string { return f.Type }

// File represents a basic file with attributes like name, path, identifier, and parentID.
type File struct {
	Id           string
	ParentId     string
	ParentPath   string // The parent's relative path (root-relative)
	DisplayName  string
	LocationPath string
	LastUpdated  string
	DepthLevel   int
	Size         int64
	Type         string // "file"
}

func (f File) ID() string       { return f.Id }
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
	ListChildren(identifier string) (ListResult, error)
	DownloadFile(identifier string) (io.ReadCloser, error)
	CreateFolder(parentId, name string) (Folder, error)
	UploadFile(destId string, content io.Reader) (File, error)
	NormalizePath(path string) string
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

// ListChildrenRequest represents a request to list children
type ListChildrenRequest struct {
	ServiceID   string
	Identifier  string
	Role        string // "source" or "destination" - used to map "spectra" to the correct world
	Offset      int    // Pagination offset (default: 0)
	Limit       int    // Pagination limit (default: 100, max: 1000)
	FoldersOnly bool   // If true, only return folders and apply limit to folders only
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
	Path        string `json:"path"`        // Absolute path to the drive (e.g., "C:\" on Windows, "/" on Unix)
	DisplayName string `json:"displayName"` // Display name (e.g., "C:" or "Local Disk (C:)")
	Type        string `json:"type"`        // Drive type (e.g., "fixed", "removable", "network")
}

// LocalServiceConfig represents configuration for a local filesystem service
type LocalServiceConfig struct {
	ID       string
	Name     string
	RootPath string // Empty means unrestricted browsing
}

// SpectraServiceConfig represents configuration for a Spectra filesystem service
type SpectraServiceConfig struct {
	ID         string
	Name       string
	World      string // "primary", "s1", "s2", etc.
	RootID     string // Typically "root"
	ConfigPath string // Path to Spectra config file
}

// ServiceDefinition represents a service definition
type ServiceDefinition struct {
	ID      string
	Name    string
	Type    ServiceType
	Local   *LocalServiceConfig
	Spectra *SpectraServiceConfig
}

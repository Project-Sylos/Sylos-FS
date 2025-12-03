// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package fs

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Project-Sylos/Spectra/sdk"
	"github.com/Project-Sylos/Sylos-FS/pkg/types"
)

// SpectraFS implements FSAdapter for Spectra filesystem simulator.
type SpectraFS struct {
	fs     *sdk.SpectraFS
	rootID string // The root node ID (now always "root" in single-table design)
	world  string // The world name ("primary", "s1", "s2", etc.) for filtering queries
}

// NewSpectraFS constructs a new SpectraFS adapter.
// rootID is the node ID (typically "root"), and world is the world name ("primary", "s1", etc.).
func NewSpectraFS(spectraFS *sdk.SpectraFS, rootID string, world string) (*SpectraFS, error) {
	if spectraFS == nil {
		return nil, fmt.Errorf("spectra filesystem instance cannot be nil")
	}

	if world == "" {
		world = "primary" // Default to primary world if not specified
	}

	// Validate that the root node exists using request struct
	rootNode, err := spectraFS.GetNode(&sdk.GetNodeRequest{
		ID: rootID,
	})
	if err != nil {
		return nil, fmt.Errorf("invalid root node ID '%s': %w", rootID, err)
	}

	if rootNode.Type != types.NodeTypeFolder {
		return nil, fmt.Errorf("root node must be a folder, got type: %s", rootNode.Type)
	}

	return &SpectraFS{
		fs:     spectraFS,
		rootID: rootID,
		world:  world,
	}, nil
}

// ListChildren lists immediate children of the given node identifier (Spectra node ID).
// It currently retrieves the full child set in one call to the Spectra SDK.
// For consistency with other services and future-proofing against backend pagination,
// callers that want to process children in fixed-size pages should wrap this with
// NewListPager(result, pageSize).
func (s *SpectraFS) ListChildren(identifier string) (types.ListResult, error) {
	var result types.ListResult

	// Get the node to verify it exists and is a folder using request struct
	parentNode, err := s.fs.GetNode(&sdk.GetNodeRequest{
		ID: identifier,
	})
	if err != nil {
		return result, err
	}

	if parentNode.Type != types.NodeTypeFolder {
		return result, fmt.Errorf("node %s is not a folder", identifier)
	}

	// List children from Spectra using request struct with world filter
	listResult, err := s.fs.ListChildren(&sdk.ListChildrenRequest{
		ParentID:  identifier,
		TableName: s.world, // Filter by world (e.g., "primary", "s1")
	})
	if err != nil {
		return result, err
	}

	// Convert Spectra nodes to our internal format
	for _, node := range listResult.Folders {
		result.Folders = append(result.Folders, types.Folder{
			Id:           node.ID,
			ParentId:     identifier,
			ParentPath:   types.NormalizeParentPath(node.ParentPath),
			DisplayName:  node.Name,
			LocationPath: types.NormalizeLocationPath(node.Path),
			LastUpdated:  node.LastUpdated.Format(time.RFC3339),
			DepthLevel:   node.DepthLevel,
			Type:         types.NodeTypeFolder,
		})
	}

	for _, node := range listResult.Files {
		result.Files = append(result.Files, types.File{
			Id:           node.ID,
			ParentId:     identifier,
			ParentPath:   types.NormalizeParentPath(node.ParentPath), // parent's relative path
			DisplayName:  node.Name,
			LocationPath: types.NormalizeLocationPath(node.Path),
			LastUpdated:  node.LastUpdated.Format(time.RFC3339),
			Size:         node.Size,
			DepthLevel:   node.DepthLevel,
			Type:         types.NodeTypeFile,
		})
	}

	return result, nil
}

// NewChildrenPager is a convenience wrapper that returns a ListPager over the
// children of a Spectra node. It mirrors the SDK-style pagination model used
// by many cloud services while keeping the FSAdapter interface simple.
func (s *SpectraFS) NewChildrenPager(identifier string, pageSize int) (*types.ListPager, error) {
	result, err := s.ListChildren(identifier)
	if err != nil {
		return nil, err
	}
	return types.NewListPager(result, pageSize), nil
}

// DownloadFile retrieves file data from Spectra and returns it as a ReadCloser.
func (s *SpectraFS) DownloadFile(identifier string) (io.ReadCloser, error) {
	// Get file data from Spectra
	fileData, _, err := s.fs.GetFileData(identifier)
	if err != nil {
		return nil, err
	}

	// Convert file data to ReadCloser
	return io.NopCloser(strings.NewReader(string(fileData))), nil
}

// CreateFolder creates a new folder under the specified parent node.
func (s *SpectraFS) CreateFolder(parentId, name string) (types.Folder, error) {
	// Create folder in Spectra using request struct with world
	node, err := s.fs.CreateFolder(&sdk.CreateFolderRequest{
		ParentID:  parentId,
		Name:      name,
		TableName: s.world, // Create in the configured world
	})
	if err != nil {
		return types.Folder{}, err
	}

	return types.Folder{
		Id:           node.ID,
		ParentId:     parentId,
		ParentPath:   types.NormalizeParentPath(node.ParentPath),
		DisplayName:  node.Name,
		LocationPath: types.NormalizeLocationPath(node.Path),
		LastUpdated:  node.LastUpdated.Format(time.RFC3339),
		DepthLevel:   node.DepthLevel,
		Type:         types.NodeTypeFolder,
	}, nil
}

// UploadFile uploads a file to Spectra under the specified parent node.
// For Spectra, destId is interpreted as the parent folder ID where the file should be uploaded.
func (s *SpectraFS) UploadFile(destId string, content io.Reader) (types.File, error) {
	// Read all content
	data, err := io.ReadAll(content)
	if err != nil {
		return types.File{}, err
	}

	// Generate a name for the file (in a real scenario, this might come from metadata)
	fileName := fmt.Sprintf("uploaded_file_%d", time.Now().Unix())

	// Upload file to Spectra using request struct with world
	// For Spectra, destId is the parent folder ID
	node, err := s.fs.UploadFile(&sdk.UploadFileRequest{
		ParentID:  destId,
		Name:      fileName,
		Data:      data,
		TableName: s.world, // Upload to the configured world
	})
	if err != nil {
		return types.File{}, err
	}

	normalizedPath := types.NormalizeLocationPath(node.Path)

	return types.File{
		Id:           node.ID,
		ParentId:     destId,
		ParentPath:   types.NormalizeParentPath(node.ParentPath),
		DisplayName:  node.Name,
		LocationPath: normalizedPath,
		LastUpdated:  node.LastUpdated.Format(time.RFC3339),
		Size:         node.Size,
		DepthLevel:   node.DepthLevel,
		Type:         types.NodeTypeFile,
	}, nil
}

// NormalizePath normalizes a Spectra node ID or path string.
func (s *SpectraFS) NormalizePath(path string) string {
	return types.NormalizeLocationPath(path)
}

// Close closes the underlying Spectra SDK instance.
// This should be called during graceful shutdown to ensure proper cleanup.
// Note: Multiple SpectraFS adapters may share the same SDK instance, so Close()
// should only be called once per SDK instance to avoid panics.
func (s *SpectraFS) Close() error {
	if s.fs != nil {
		return s.fs.Close()
	}
	return nil
}

// GetSDKInstance returns the underlying Spectra SDK instance.
// This is used to check if multiple adapters share the same instance.
func (s *SpectraFS) GetSDKInstance() *sdk.SpectraFS {
	return s.fs
}

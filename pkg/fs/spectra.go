// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package fs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
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

// newSpectraFS creates a SpectraFS adapter from a session-owned SDK instance.
// The adapter does not own the lifecycle of the SDK instance - the session does.
// This function does NOT validate the root node - it assumes the session is valid.
func NewSpectraFS(spectraFS *sdk.SpectraFS, rootID string, world string) (*SpectraFS, error) {
	if spectraFS == nil {
		return nil, fmt.Errorf("spectra filesystem instance cannot be nil")
	}

	if world == "" {
		world = "primary" // Default to primary world if not specified
	}

	// Note: We do NOT validate the root node here because:
	// 1. The session is responsible for ensuring the SDK is valid
	// 2. Validation will happen on first use (e.g., ListChildren)
	// 3. This matches the Migration-Engine pattern of creating adapters without validation

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
			ServiceID:    node.ID,
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
			ServiceID:    node.ID,
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

// OpenRead retrieves file data from Spectra and returns a readable stream.
// The worker owns the copy loop - this just provides the stream.
func (s *SpectraFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	// Get file data from Spectra
	fileData, _, err := s.fs.GetFileData(fileID)
	if err != nil {
		return nil, err
	}

	// Return plain reader - worker handles the copy loop
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
		ServiceID:    node.ID,
		ParentId:     parentId,
		ParentPath:   types.NormalizeParentPath(node.ParentPath),
		DisplayName:  node.Name,
		LocationPath: types.NormalizeLocationPath(node.Path),
		LastUpdated:  node.LastUpdated.Format(time.RFC3339),
		DepthLevel:   node.DepthLevel,
		Type:         types.NodeTypeFolder,
	}, nil
}

// CreateFile creates a file entry in Spectra with metadata only.
// The actual file data will be uploaded when OpenWrite().Close() is called.
// This avoids uploading empty data, which the Spectra SDK now rejects.
func (s *SpectraFS) CreateFile(ctx context.Context, parentID, name string, size int64, metadata map[string]string) (types.File, error) {
	// Don't upload anything yet - just return metadata.
	// The file will be created with actual data when OpenWrite().Close() is called.
	// We encode parentID and name in the ServiceID so OpenWrite can retrieve them.
	// Format: "pending:<parentID>:<name>" - this is a temporary identifier.

	// Get parent node to construct the path
	parentNode, err := s.fs.GetNode(&sdk.GetNodeRequest{
		ID: parentID,
	})
	if err != nil {
		return types.File{}, fmt.Errorf("failed to get parent node: %w", err)
	}

	// Construct the expected path (Spectra will generate the actual path on upload)
	parentPath := types.NormalizeParentPath(parentNode.ParentPath)
	var expectedPath string
	if parentPath == "/" {
		expectedPath = "/" + name
	} else {
		expectedPath = parentPath + "/" + name
	}

	// Return a File with a pending ServiceID that encodes parentID and name
	// Format: "pending:<parentID>:<name>" - OpenWrite will parse this
	pendingID := fmt.Sprintf("pending:%s:%s", parentID, name)

	return types.File{
		ServiceID:    pendingID, // Temporary ID - will be replaced when file is actually created
		ParentId:     parentID,
		ParentPath:   parentPath,
		DisplayName:  name,
		LocationPath: types.NormalizeLocationPath(expectedPath),
		LastUpdated:  time.Now().Format(time.RFC3339),
		Size:         size,                      // Use provided size (may be 0 initially)
		DepthLevel:   parentNode.DepthLevel + 1, // Child is one level deeper
		Type:         types.NodeTypeFile,
	}, nil
}

// OpenWrite returns a WriteCloser that buffers writes internally.
// Since Spectra SDK requires all data upfront, this buffers in memory or to a temp file.
// On Close(), it creates the file in Spectra with the actual data.
// The fileID must be a "pending:" ID returned from CreateFile().
func (s *SpectraFS) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	if !strings.HasPrefix(fileID, "pending:") {
		return nil, fmt.Errorf("OpenWrite requires a pending file ID from CreateFile(), got: %s", fileID)
	}
	return newSpectraWriteCloser(s, fileID), nil
}

// spectraWriteCloser buffers writes and uploads to Spectra on Close().
type spectraWriteCloser struct {
	spectraFS *SpectraFS
	fileID    string
	buffer    *bytes.Buffer
	tempFile  *os.File
	useTemp   bool
	mu        sync.Mutex
	closed    bool
}

// newSpectraWriteCloser creates a new buffering WriteCloser for Spectra.
// It uses memory buffer initially, and can spill to temp file for large writes.
func newSpectraWriteCloser(spectraFS *SpectraFS, fileID string) *spectraWriteCloser {
	return &spectraWriteCloser{
		spectraFS: spectraFS,
		fileID:    fileID,
		buffer:    &bytes.Buffer{},
		useTemp:   false,
		closed:    false,
	}
}

// Write buffers data. For large files, it may spill to a temp file.
func (swc *spectraWriteCloser) Write(p []byte) (int, error) {
	swc.mu.Lock()
	defer swc.mu.Unlock()

	if swc.closed {
		return 0, fmt.Errorf("write to closed writer")
	}

	// Use memory buffer for small files (< 10MB)
	// For larger files, spill to temp file
	const memoryThreshold = 10 * 1024 * 1024 // 10MB

	if !swc.useTemp && swc.buffer.Len()+len(p) > memoryThreshold {
		// Spill to temp file
		tempFile, err := os.CreateTemp("", "spectra-upload-*")
		if err != nil {
			return 0, fmt.Errorf("failed to create temp file: %w", err)
		}

		// Write existing buffer to temp file
		if _, err := tempFile.Write(swc.buffer.Bytes()); err != nil {
			tempFile.Close()
			os.Remove(tempFile.Name())
			return 0, fmt.Errorf("failed to write buffer to temp file: %w", err)
		}

		swc.tempFile = tempFile
		swc.buffer = nil
		swc.useTemp = true
	}

	if swc.useTemp {
		return swc.tempFile.Write(p)
	}

	return swc.buffer.Write(p)
}

// Close uploads the buffered data to Spectra, creating the file with actual data.
// The fileID must be a "pending:" ID from CreateFile().
func (swc *spectraWriteCloser) Close() error {
	swc.mu.Lock()
	defer swc.mu.Unlock()

	if swc.closed {
		return nil
	}
	swc.closed = true

	var data []byte
	var err error

	if swc.useTemp {
		// Read from temp file
		if swc.tempFile != nil {
			// Seek to beginning
			if _, err := swc.tempFile.Seek(0, 0); err != nil {
				swc.tempFile.Close()
				os.Remove(swc.tempFile.Name())
				return fmt.Errorf("failed to seek temp file: %w", err)
			}

			// Read all data
			data, err = io.ReadAll(swc.tempFile)
			swc.tempFile.Close()
			tempName := swc.tempFile.Name()
			os.Remove(tempName) // Clean up temp file

			if err != nil {
				return fmt.Errorf("failed to read temp file: %w", err)
			}
		}
	} else {
		// Read from memory buffer
		data = swc.buffer.Bytes()
	}

	// Parse the pending ID to get parentID and name
	// Format: "pending:<parentID>:<name>"
	if !strings.HasPrefix(swc.fileID, "pending:") {
		return fmt.Errorf("invalid file ID format: expected pending ID, got %s", swc.fileID)
	}

	parts := strings.SplitN(swc.fileID, ":", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid pending file ID format: %s", swc.fileID)
	}
	parentID := parts[1]
	name := parts[2]

	// Create the file with actual data
	_, err = swc.spectraFS.fs.UploadFile(&sdk.UploadFileRequest{
		ParentID:  parentID,
		Name:      name,
		Data:      data,
		TableName: swc.spectraFS.world,
	})
	if err != nil {
		return fmt.Errorf("failed to create file with data: %w", err)
	}

	return nil
}

// NormalizePath normalizes a Spectra node ID or path string.
func (s *SpectraFS) NormalizePath(path string) string {
	return types.NormalizeLocationPath(path)
}

// GetSDKInstance returns the underlying Spectra SDK instance.
// This is used to check if multiple adapters share the same instance.
func (s *SpectraFS) GetSDKInstance() *sdk.SpectraFS {
	return s.fs
}

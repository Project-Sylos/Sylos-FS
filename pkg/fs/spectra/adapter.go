// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package spectra

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"codeberg.org/Sylos/Spectra/sdk"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// SpectraFS implements FSAdapter for Spectra filesystem simulator.
type SpectraFS struct {
	fs          *sdk.SpectraFS
	rootID      string // The root node ID (now always "root" in single-table design)
	world       string // The world name ("primary", "s1", "s2", etc.) for filtering queries
	isEphemeral bool   // true if in ephemeral mode, false for persistent mode
	degradation *types.FSDegradationState
	types.ConcurrencyHint
}

// SpectraFSOption configures optional SpectraFS behavior.
type SpectraFSOption func(*SpectraFS)

// WithDegradationState attaches shared degradation telemetry to the adapter.
func WithDegradationState(state *types.FSDegradationState) SpectraFSOption {
	return func(s *SpectraFS) {
		s.degradation = state
	}
}

// newSpectraFS creates a SpectraFS adapter from a session-owned SDK instance.
// The adapter does not own the lifecycle of the SDK instance - the session does.
// This function does NOT validate the root node - it assumes the session is valid.
func NewSpectraFS(spectraFS *sdk.SpectraFS, rootID string, world string, isEphemeral bool, opts ...SpectraFSOption) (*SpectraFS, error) {
	if spectraFS == nil {
		return nil, fmt.Errorf("spectra filesystem instance cannot be nil")
	}

	if world == "" {
		world = "primary" // Default to primary world if not specified
	}

	s := &SpectraFS{
		fs:          spectraFS,
		rootID:      rootID,
		world:       world,
		isEphemeral: isEphemeral,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.degradation == nil {
		s.degradation = types.NewFSDegradationState()
	}

	if spectraFS.AuthEnabled() {
		if _, err := spectraFS.EnsureWorldAuth(world); err != nil {
			return nil, fmt.Errorf("ensure auth for world %s: %w", world, err)
		}
	}

	return s, nil
}

// ListChildren lists immediate children of the given node identifier (Spectra node ID).
// It currently retrieves the full child set in one call to the Spectra SDK.
// For consistency with other services and future-proofing against backend pagination,
// callers that want to process children in fixed-size pages should wrap this with
// NewListPager(result, pageSize).
// For ephemeral mode, depth and parentPath must be provided by the caller (no persisted node to read from).
// For persistent mode, depth and parentPath are optional.
func (s *SpectraFS) ListChildren(ctx context.Context, identifier string, depth *int, parentPath string) (types.ListResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var result types.ListResult

	// For ephemeral mode, depth and parentPath are required (caller must supply; nothing persisted)
	if s.isEphemeral {
		if depth == nil {
			return result, fmt.Errorf("depth parameter is required for ephemeral mode")
		}
		if parentPath == "" {
			return result, fmt.Errorf("path is required in ephemeral mode (use parent_path)")
		}
		// Build request from caller-supplied path and depth; no GetNode in ephemeral mode
		pathStr := types.NormalizeLocationPath(parentPath)
		if pathStr == "" {
			pathStr = "/"
		}
		req := &sdk.ListChildrenRequest{
			ParentID:   identifier,
			ParentPath: pathStr,
			TableName:  s.world,
			Depth:      depth,
		}
		listResult, err := s.listChildrenWithRetry(ctx, req)
		if err != nil {
			return result, err
		}
		return s.convertListResult(listResult, identifier), nil
	}

	// Persistent mode: optional depth, no parentPath needed; validate node via GetNode
	parentNode, err := s.getNodeWithRetry(ctx, identifier)
	if err != nil {
		return result, err
	}

	if parentNode.Type != types.NodeTypeFolder {
		return result, fmt.Errorf("node %s is not a folder", identifier)
	}

	req := &sdk.ListChildrenRequest{
		ParentID:  identifier,
		TableName: s.world,
	}
	if depth != nil {
		req.Depth = depth
	}

	listResult, err := s.listChildrenWithRetry(ctx, req)
	if err != nil {
		return result, err
	}

	return s.convertListResult(listResult, identifier), nil
}

func (s *SpectraFS) listChildrenWithRetry(ctx context.Context, req *sdk.ListChildrenRequest) (*sdk.ListResult, error) {
	var out *sdk.ListResult
	err := s.withClassifiedRetry(ctx, "ListChildren", func() error {
		res, callErr := s.fs.ListChildren(req)
		if callErr != nil {
			return callErr
		}
		out = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// convertListResult maps Spectra list response to types.ListResult.
func (s *SpectraFS) convertListResult(listResult *sdk.ListResult, identifier string) types.ListResult {
	var result types.ListResult
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
			ParentPath:   types.NormalizeParentPath(node.ParentPath),
			DisplayName:  node.Name,
			LocationPath: types.NormalizeLocationPath(node.Path),
			LastUpdated:  node.LastUpdated.Format(time.RFC3339),
			Size:         node.Size,
			DepthLevel:   node.DepthLevel,
			Type:         types.NodeTypeFile,
		})
	}
	return result
}

// NewChildrenPager is a convenience wrapper that returns a ListPager over the
// children of a Spectra node. It mirrors the SDK-style pagination model used
// by many cloud services while keeping the FSAdapter interface simple.
func (s *SpectraFS) NewChildrenPager(ctx context.Context, identifier string, pageSize int, depth *int, parentPath string) (*types.ListPager, error) {
	result, err := s.ListChildren(ctx, identifier, depth, parentPath)
	if err != nil {
		return nil, err
	}
	return types.NewListPager(result, pageSize), nil
}

// OpenRead retrieves file data from Spectra and returns a readable stream.
// The worker owns the copy loop - this just provides the stream.
func (s *SpectraFS) OpenRead(ctx context.Context, fileID string) (io.ReadCloser, error) {
	var fileData []byte
	err := s.withClassifiedRetry(ctx, "GetFileData", func() error {
		data, _, callErr := s.fs.GetFileData(fileID, s.world)
		if callErr != nil {
			return callErr
		}
		fileData = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(string(fileData))), nil
}

// CreateFolder creates a new folder under the specified parent node.
func (s *SpectraFS) CreateFolder(ctx context.Context, parentId, name string, _ map[string]string) (types.Folder, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var node *sdk.Node
	err := s.withClassifiedRetry(ctx, "CreateFolder", func() error {
		n, callErr := s.fs.CreateFolder(&sdk.CreateFolderRequest{
			ParentID:  parentId,
			Name:      name,
			TableName: s.world,
		})
		if callErr != nil {
			return callErr
		}
		node = n
		return nil
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

// DeleteNode removes a file or folder from Spectra.
func (s *SpectraFS) DeleteNode(ctx context.Context, nodeID string, nodeType string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("spectra: node id is required")
	}
	_ = nodeType
	return s.withClassifiedRetry(ctx, "DeleteNode", func() error {
		return s.fs.DeleteNode(&sdk.DeleteNodeRequest{
			ID:        nodeID,
			TableName: s.world,
		})
	})
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
	parentNode, err := s.getNodeWithRetry(ctx, parentID)
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

// OpenWrite returns a WriteCloser that accepts a byte stream without staging.
// Spectra's UploadFile does not persist request bytes (deterministic fake content),
// so Write only counts/accepts the stream; Close registers the node.
func (s *SpectraFS) OpenWrite(ctx context.Context, fileID string) (io.WriteCloser, error) {
	_ = ctx
	if !strings.HasPrefix(fileID, "pending:") {
		return nil, fmt.Errorf("OpenWrite requires a pending file ID from CreateFile(), got: %s", fileID)
	}
	return newSpectraWriteCloser(s, fileID), nil
}

// spectraWriteCloser accepts streamed bytes without buffering to memory or disk.
type spectraWriteCloser struct {
	spectraFS *SpectraFS
	fileID    string
	mu        sync.Mutex
	closed    bool
	accepted  int64
}

func newSpectraWriteCloser(spectraFS *SpectraFS, fileID string) *spectraWriteCloser {
	return &spectraWriteCloser{
		spectraFS: spectraFS,
		fileID:    fileID,
	}
}

func (swc *spectraWriteCloser) Write(p []byte) (int, error) {
	swc.mu.Lock()
	defer swc.mu.Unlock()
	if swc.closed {
		return 0, fmt.Errorf("write to closed writer")
	}
	swc.accepted += int64(len(p))
	return len(p), nil
}

func (swc *spectraWriteCloser) Close() error {
	swc.mu.Lock()
	defer swc.mu.Unlock()

	if swc.closed {
		return nil
	}
	swc.closed = true

	if !strings.HasPrefix(swc.fileID, "pending:") {
		return fmt.Errorf("invalid file ID format: expected pending ID, got %s", swc.fileID)
	}

	parts := strings.SplitN(swc.fileID, ":", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid pending file ID format: %s", swc.fileID)
	}
	parentID := parts[1]
	name := parts[2]

	// SDK requires non-empty Data but does not persist it; placeholder is enough.
	placeholder := []byte{0}
	if swc.accepted == 0 {
		placeholder = []byte{0}
	}

	uploadErr := swc.spectraFS.withClassifiedRetry(context.Background(), "UploadFile", func() error {
		_, innerErr := swc.spectraFS.fs.UploadFile(&sdk.UploadFileRequest{
			ParentID:  parentID,
			Name:      name,
			Data:      placeholder,
			TableName: swc.spectraFS.world,
		})
		return innerErr
	})
	if uploadErr != nil {
		return fmt.Errorf("failed to create file with data: %w", uploadErr)
	}

	return nil
}

// NormalizePath normalizes a Spectra node ID or path string.
func (s *SpectraFS) NormalizePath(path string) string {
	return types.NormalizeLocationPath(path)
}

// Initialize is a no-op for SpectraFS.
func (s *SpectraFS) Initialize(_ []byte, _ string) error {
	return nil
}

// RegisterCredentials is a no-op for SpectraFS.
func (s *SpectraFS) RegisterCredentials(_ []byte, _ []byte, _ string) error {
	return nil
}

// HasValidCredentials reports whether auth credentials are bound when auth is enabled.
func (s *SpectraFS) HasValidCredentials() bool {
	if s == nil || s.fs == nil {
		return false
	}
	if !s.fs.AuthEnabled() {
		return true
	}
	return s.fs.AuthEngine() != nil && s.fs.AuthEngine().HasPresentedToken(s.world)
}

// DegradationState implements types.FSDegradationReporter.
func (s *SpectraFS) DegradationState() types.FSDegradationSnapshot {
	if s.degradation == nil {
		return types.FSDegradationSnapshot{}
	}
	return s.degradation.DegradationState()
}

// RecordSignal implements types.FSDegradationReporter.
func (s *SpectraFS) RecordSignal(sig types.FSDegradationSignal) {
	if s.degradation != nil {
		s.degradation.RecordSignal(sig)
	}
}

// GetDegradationState returns the shared degradation tracker, if any.
func (s *SpectraFS) GetDegradationState() *types.FSDegradationState {
	return s.degradation
}

// GetSDKInstance returns the underlying Spectra SDK instance.
// This is used to check if multiple adapters share the same instance.
func (s *SpectraFS) GetSDKInstance() *sdk.SpectraFS {
	return s.fs
}

const (
	spectraListPageMin     = 20
	spectraListPageMax     = 10000
	spectraListPageDefault = 100
)

// ListChildrenPagination implements types.FSListChildrenPagination.
func (s *SpectraFS) ListChildrenPagination() types.ListChildrenPagination {
	return types.ListChildrenPagination{
		MinPageSize:                   spectraListPageMin,
		MaxPageSize:                   spectraListPageMax,
		DefaultPageSize:               spectraListPageDefault,
		PreferLargePagesUnderThrottle: true,
	}
}

var (
	_ types.FSAdapter                = (*SpectraFS)(nil)
	_ types.FSDegradationReporter    = (*SpectraFS)(nil)
	_ types.FSListChildrenPagination = (*SpectraFS)(nil)
	_ types.FSStorageInfo            = (*SpectraFS)(nil)
)

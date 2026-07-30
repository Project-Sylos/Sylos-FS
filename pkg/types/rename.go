// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
	"context"
	"fmt"
)

// ErrRenameUnsupported is returned when an adapter does not implement FSRename.
var ErrRenameUnsupported = fmt.Errorf("rename is not supported by this filesystem adapter")

// RenameResult is the outcome of renaming a node on the destination.
type RenameResult struct {
	ServiceID   string
	DisplayName string
	// LocationPath is optional; adapters may leave empty when path is ME-managed.
	LocationPath string
}

// FSRename is implemented by adapters that can rename a file or folder in place.
// parentServiceID is the parent folder's service id (empty for provider root as defined by the adapter).
// serviceID is the node to rename; newName is the destination basename only.
type FSRename interface {
	RenameNode(ctx context.Context, parentServiceID, serviceID, newName, nodeType string) (RenameResult, error)
}

// RenameFrom returns an FSRename when the adapter implements it.
func RenameFrom(adapter FSAdapter) (FSRename, bool) {
	if adapter == nil {
		return nil, false
	}
	r, ok := adapter.(FSRename)
	return r, ok && r != nil
}

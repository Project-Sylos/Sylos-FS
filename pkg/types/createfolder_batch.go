// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import "context"

// DefaultCreateFolderBatchPullSize is the suggested DuckDB→pendingBuff fill when
// the destination supports CreateFolderBatch (keeps workers supplied for fat groups).
const DefaultCreateFolderBatchPullSize = 20_000

// DefaultCreateFolderBatchMax is the default per-RPC folder count when an adapter
// does not override CreateFolderBatchMax (Dropbox delete_batch caps at 1000).
const DefaultCreateFolderBatchMax = 1000

// CreateFolderBatchItem is one folder create within a batch RPC.
type CreateFolderBatchItem struct {
	ParentID string
	Name     string
	Metadata map[string]string
}

// CreateFolderBatchEntryResult is the positional outcome for one CreateFolderBatchItem.
// Err nil means success and Folder is populated.
type CreateFolderBatchEntryResult struct {
	Folder Folder
	Err    error
}

// FSCreateFolderBatch is implemented by adapters that can create many folders in one RPC.
type FSCreateFolderBatch interface {
	CreateFolderBatchMax() int
	CreateFolderBatch(ctx context.Context, items []CreateFolderBatchItem) ([]CreateFolderBatchEntryResult, error)
}

// CreateFolderBatchFrom returns the batch create capability when the adapter supports it.
func CreateFolderBatchFrom(adapter any) (FSCreateFolderBatch, bool) {
	if adapter == nil {
		return nil, false
	}
	b, ok := adapter.(FSCreateFolderBatch)
	return b, ok && b != nil
}

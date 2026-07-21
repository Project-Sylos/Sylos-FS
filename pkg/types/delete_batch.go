// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import "context"

// DefaultDeleteBatchPullSize is the suggested DuckDB→pendingBuff fill when the
// source supports DeleteBatch.
const DefaultDeleteBatchPullSize = 20_000

// DefaultDeleteBatchMax matches Dropbox files/delete_batch entry cap.
const DefaultDeleteBatchMax = 1000

// DeleteBatchItem is one node delete within a batch RPC.
type DeleteBatchItem struct {
	NodeID   string
	NodeType string // folder|file
}

// DeleteBatchEntryResult is the positional outcome for one DeleteBatchItem.
// Err nil means success.
type DeleteBatchEntryResult struct {
	Err error
}

// FSDeleteBatch is implemented by adapters that can delete many nodes in one RPC.
type FSDeleteBatch interface {
	DeleteBatchMax() int
	DeleteBatch(ctx context.Context, items []DeleteBatchItem) ([]DeleteBatchEntryResult, error)
}

// DeleteBatchFrom returns the batch delete capability when the adapter supports it.
func DeleteBatchFrom(adapter any) (FSDeleteBatch, bool) {
	if adapter == nil {
		return nil, false
	}
	b, ok := adapter.(FSDeleteBatch)
	return b, ok && b != nil
}

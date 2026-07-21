// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
	"context"
	"io"
)

// DefaultUploadFilesBatchPullSize is the suggested DuckDB→pendingBuff fill when
// the destination supports UploadFilesBatch (keeps workers supplied for fat groups).
const DefaultUploadFilesBatchPullSize = 20_000

// DefaultUploadFilesBatchMax is the default per-RPC file count when an adapter
// does not override UploadFilesBatchMax. Cap is below Dropbox's finish_batch
// limit of 1000 so long multi-file commits stay bounded.
const DefaultUploadFilesBatchMax = 500

// UploadFilesBatchItem is one file upload within a batch commit.
// Body is read and closed by the adapter (caller must not reuse it after the call).
type UploadFilesBatchItem struct {
	ParentID string
	Name     string
	Body     io.ReadCloser
	Metadata map[string]string
}

// UploadFilesBatchEntryResult is the positional outcome for one UploadFilesBatchItem.
// Err nil means success and File is populated with the committed service id.
type UploadFilesBatchEntryResult struct {
	File File
	Err  error
}

// FSUploadFilesBatch is implemented by adapters that can commit many upload sessions in one RPC
// (start sessions, append bytes, finish_batch).
type FSUploadFilesBatch interface {
	UploadFilesBatchMax() int
	UploadFilesBatch(ctx context.Context, items []UploadFilesBatchItem) ([]UploadFilesBatchEntryResult, error)
}

// UploadFilesBatchFrom returns the batch upload capability when the adapter supports it.
func UploadFilesBatchFrom(adapter any) (FSUploadFilesBatch, bool) {
	if adapter == nil {
		return nil, false
	}
	b, ok := adapter.(FSUploadFilesBatch)
	return b, ok && b != nil
}

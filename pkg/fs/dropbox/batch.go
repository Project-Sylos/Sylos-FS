// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

const (
	dropboxBatchMaxEntries       = 1000 // create_folder_batch / delete_batch API max
	dropboxUploadBatchMaxEntries = 500  // ME/UI bound below finish_batch's 1000 API max
	batchPollInterval            = 500 * time.Millisecond
	batchPollMaxWait             = 10 * time.Minute
)

type createFolderBatchArg struct {
	Paths      []string `json:"paths"`
	Autorename bool     `json:"autorename"`
	ForceAsync bool     `json:"force_async"`
}

type deleteBatchArg struct {
	Entries []deleteArg `json:"entries"`
}

type batchLaunch struct {
	Tag         string          `json:".tag"`
	AsyncJobID  string          `json:"async_job_id"`
	Entries     json.RawMessage `json:"entries"`
}

type batchJobStatus struct {
	Tag     string          `json:".tag"`
	Entries json.RawMessage `json:"entries"`
}

type batchEntryTagged struct {
	Tag      string          `json:".tag"`
	Metadata json.RawMessage `json:"metadata"`
	Failure  json.RawMessage `json:"failure"`
}

func (c *Client) createFolderBatch(ctx context.Context, paths []string) ([]batchEntryTagged, error) {
	var launch batchLaunch
	err := c.rpc(ctx, "files/create_folder_batch", createFolderBatchArg{
		Paths:      paths,
		Autorename: false,
		ForceAsync: false,
	}, &launch)
	if err != nil {
		return nil, err
	}
	return c.resolveBatchEntries(ctx, "files/create_folder_batch/check", launch)
}

func (c *Client) deleteBatch(ctx context.Context, paths []string) ([]batchEntryTagged, error) {
	entries := make([]deleteArg, len(paths))
	for i, p := range paths {
		entries[i] = deleteArg{Path: p}
	}
	var launch batchLaunch
	err := c.rpc(ctx, "files/delete_batch", deleteBatchArg{Entries: entries}, &launch)
	if err != nil {
		return nil, err
	}
	return c.resolveBatchEntries(ctx, "files/delete_batch/check", launch)
}

type uploadSessionStartBatchArg struct {
	NumSessions uint64 `json:"num_sessions"`
}

type uploadSessionStartBatchResult struct {
	SessionIDs []string `json:"session_ids"`
}

type uploadSessionCursor struct {
	SessionID string `json:"session_id"`
	Offset    uint64 `json:"offset"`
}

type uploadSessionFinishArg struct {
	Cursor uploadSessionCursor `json:"cursor"`
	Commit uploadArg           `json:"commit"`
}

type uploadSessionFinishBatchArg struct {
	Entries []uploadSessionFinishArg `json:"entries"`
}

func (c *Client) uploadSessionStartBatch(ctx context.Context, numSessions int) ([]string, error) {
	if numSessions <= 0 {
		return nil, nil
	}
	var out uploadSessionStartBatchResult
	err := c.rpc(ctx, "files/upload_session/start_batch", uploadSessionStartBatchArg{
		NumSessions: uint64(numSessions),
	}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.SessionIDs) != numSessions {
		return nil, fmt.Errorf("dropbox: start_batch returned %d session ids for %d sessions", len(out.SessionIDs), numSessions)
	}
	return out.SessionIDs, nil
}

func (c *Client) uploadSessionFinishBatch(ctx context.Context, entries []uploadSessionFinishArg) ([]batchEntryTagged, error) {
	var launch batchLaunch
	err := c.rpc(ctx, "files/upload_session/finish_batch", uploadSessionFinishBatchArg{Entries: entries}, &launch)
	if err != nil {
		return nil, err
	}
	return c.resolveBatchEntries(ctx, "files/upload_session/finish_batch/check", launch)
}

func (c *Client) resolveBatchEntries(ctx context.Context, checkEndpoint string, launch batchLaunch) ([]batchEntryTagged, error) {
	tag := strings.TrimSpace(launch.Tag)
	switch tag {
	case "complete":
		return decodeBatchEntries(launch.Entries)
	case "async_job_id":
		jobID := strings.TrimSpace(launch.AsyncJobID)
		if jobID == "" {
			return nil, fmt.Errorf("dropbox: batch returned async_job_id with empty id")
		}
		deadline := time.Now().Add(batchPollMaxWait)
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("dropbox: batch job %s timed out after %s", jobID, batchPollMaxWait)
			}
			var status batchJobStatus
			err := c.rpc(ctx, checkEndpoint, map[string]string{"async_job_id": jobID}, &status)
			if err != nil {
				return nil, err
			}
			switch strings.TrimSpace(status.Tag) {
			case "complete":
				return decodeBatchEntries(status.Entries)
			case "failed":
				return nil, fmt.Errorf("dropbox: batch job %s failed", jobID)
			case "in_progress":
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(batchPollInterval):
				}
			default:
				return nil, fmt.Errorf("dropbox: unexpected batch check tag %q", status.Tag)
			}
		}
	default:
		return nil, fmt.Errorf("dropbox: unexpected batch launch tag %q", tag)
	}
}

func decodeBatchEntries(raw json.RawMessage) ([]batchEntryTagged, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("dropbox: batch complete with empty entries")
	}
	var entries []batchEntryTagged
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("dropbox: decode batch entries: %w", err)
	}
	return entries, nil
}

func batchEntryFailureError(entry batchEntryTagged) error {
	tag := strings.TrimSpace(entry.Tag)
	summary := tag
	if len(entry.Failure) > 0 {
		var fail taggedError
		if json.Unmarshal(entry.Failure, &fail) == nil && fail.Tag != "" {
			summary = fail.Tag
		} else {
			summary = string(entry.Failure)
		}
	}
	return &APIError{
		Status:       http.StatusConflict,
		ErrorSummary: summary,
		ErrorTag:     summary,
		Body:         entry.Failure,
	}
}

// CreateFolderBatchMax implements types.FSCreateFolderBatch.
func (d *DropboxFS) CreateFolderBatchMax() int {
	return dropboxBatchMaxEntries
}

// CreateFolderBatch implements types.FSCreateFolderBatch.
func (d *DropboxFS) CreateFolderBatch(ctx context.Context, items []types.CreateFolderBatchItem) ([]types.CreateFolderBatchEntryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) > dropboxBatchMaxEntries {
		return nil, fmt.Errorf("dropbox: CreateFolderBatch max is %d (got %d)", dropboxBatchMaxEntries, len(items))
	}

	paths := make([]string, len(items))
	basePaths := make([]string, len(items))
	for i, item := range items {
		if err := d.errTeamSpaceRootWrite(); err != nil && isDropboxRootRef(item.ParentID) {
			return nil, err
		}
		folderPath, err := d.createAPIPath(item.ParentID, item.Name, item.Metadata)
		if err != nil {
			return nil, fmt.Errorf("dropbox: batch item %d: %w", i, err)
		}
		paths[i] = folderPath
		basePaths[i] = types.LogicalParentFromCreateMetadata(item.Metadata, types.NormalizeLocationPath(d.root.LocationPath))
	}

	var entries []batchEntryTagged
	err := d.withClassifiedRetry(ctx, "CreateFolderBatch", func() error {
		client, err := d.client(ctx)
		if err != nil {
			return err
		}
		entries, err = client.createFolderBatch(ctx, paths)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(entries) != len(items) {
		return nil, fmt.Errorf("dropbox: create_folder_batch returned %d entries for %d items", len(entries), len(items))
	}

	out := make([]types.CreateFolderBatchEntryResult, len(items))
	for i, entry := range entries {
		switch strings.TrimSpace(entry.Tag) {
		case "success":
			var meta fileMetadata
			if err := json.Unmarshal(entry.Metadata, &meta); err != nil {
				out[i].Err = fmt.Errorf("dropbox: decode success metadata: %w", err)
				continue
			}
			base := basePaths[i]
			if items[i].Metadata == nil || (strings.TrimSpace(items[i].Metadata["location_path"]) == "" && strings.TrimSpace(items[i].Metadata["parent_path"]) == "") {
				base = folderBasePath(meta, base)
			}
			out[i].Folder = d.metaToFolder(meta, base)
		default:
			out[i].Err = batchEntryFailureError(entry)
		}
	}
	return out, nil
}

// DeleteBatchMax implements types.FSDeleteBatch.
func (d *DropboxFS) DeleteBatchMax() int {
	return dropboxBatchMaxEntries
}

// DeleteBatch implements types.FSDeleteBatch.
func (d *DropboxFS) DeleteBatch(ctx context.Context, items []types.DeleteBatchItem) ([]types.DeleteBatchEntryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) > dropboxBatchMaxEntries {
		return nil, fmt.Errorf("dropbox: DeleteBatch max is %d (got %d)", dropboxBatchMaxEntries, len(items))
	}

	paths := make([]string, len(items))
	for i, item := range items {
		id := strings.TrimSpace(item.NodeID)
		if id == "" {
			return nil, fmt.Errorf("dropbox: delete batch item %d missing node id", i)
		}
		paths[i] = dropboxPathRef(id)
	}

	var entries []batchEntryTagged
	err := d.withClassifiedRetry(ctx, "DeleteBatch", func() error {
		client, err := d.client(ctx)
		if err != nil {
			return err
		}
		entries, err = client.deleteBatch(ctx, paths)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(entries) != len(items) {
		return nil, fmt.Errorf("dropbox: delete_batch returned %d entries for %d items", len(entries), len(items))
	}

	out := make([]types.DeleteBatchEntryResult, len(items))
	for i, entry := range entries {
		switch strings.TrimSpace(entry.Tag) {
		case "success":
			// ok
		default:
			out[i].Err = batchEntryFailureError(entry)
		}
	}
	return out, nil
}

const uploadAppendConcurrency = 4

// UploadFilesBatchMax implements types.FSUploadFilesBatch.
func (d *DropboxFS) UploadFilesBatchMax() int {
	return dropboxUploadBatchMaxEntries
}

// UploadFilesBatch implements types.FSUploadFilesBatch.
// Starts sessions via start_batch, appends file bytes (limited concurrency), then finish_batch under a mutex.
// Bodies are closed by this method. Whole-batch retry is not applied (streams are not rewindable);
// throttle/auth errors propagate so ME can rate-limit or re-lease tasks.
func (d *DropboxFS) UploadFilesBatch(ctx context.Context, items []types.UploadFilesBatchItem) ([]types.UploadFilesBatchEntryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) > dropboxUploadBatchMaxEntries {
		return nil, fmt.Errorf("dropbox: UploadFilesBatch max is %d (got %d)", dropboxUploadBatchMaxEntries, len(items))
	}

	out := make([]types.UploadFilesBatchEntryResult, len(items))
	paths := make([]string, len(items))
	basePaths := make([]string, len(items))
	for i, item := range items {
		if err := d.errTeamSpaceRootWrite(); err != nil && isDropboxRootRef(item.ParentID) {
			closeUploadBatchBodies(items)
			return nil, err
		}
		filePath, err := d.createAPIPath(item.ParentID, item.Name, item.Metadata)
		if err != nil {
			closeUploadBatchBodies(items)
			return nil, fmt.Errorf("dropbox: upload batch item %d: %w", i, err)
		}
		paths[i] = filePath
		basePaths[i] = types.LogicalParentFromCreateMetadata(item.Metadata, types.NormalizeLocationPath(d.root.LocationPath))
	}

	client, err := d.client(ctx)
	if err != nil {
		closeUploadBatchBodies(items)
		return nil, err
	}

	var sessionIDs []string
	err = d.withClassifiedRetry(ctx, "UploadFilesBatchStart", func() error {
		var serr error
		sessionIDs, serr = client.uploadSessionStartBatch(ctx, len(items))
		return serr
	})
	if err != nil {
		closeUploadBatchBodies(items)
		return nil, err
	}

	type appendOutcome struct {
		offset uint64
		err    error
	}
	outcomes := make([]appendOutcome, len(items))
	sem := make(chan struct{}, uploadAppendConcurrency)
	var wg sync.WaitGroup
	for i := range items {
		i := i
		body := items[i].Body
		if body == nil {
			body = io.NopCloser(strings.NewReader(""))
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer body.Close() //nolint:errcheck
			sem <- struct{}{}
			defer func() { <-sem }()
			offset, aerr := client.appendUploadSession(ctx, sessionIDs[i], body)
			outcomes[i] = appendOutcome{offset: offset, err: aerr}
		}()
	}
	wg.Wait()

	finishEntries := make([]uploadSessionFinishArg, 0, len(items))
	finishIdx := make([]int, 0, len(items))
	for i := range items {
		if outcomes[i].err != nil {
			out[i].Err = outcomes[i].err
			continue
		}
		finishIdx = append(finishIdx, i)
		finishEntries = append(finishEntries, uploadSessionFinishArg{
			Cursor: uploadSessionCursor{SessionID: sessionIDs[i], Offset: outcomes[i].offset},
			Commit: uploadArg{
				Path:       paths[i],
				Mode:       "overwrite",
				Autorename: false,
				Mute:       false,
			},
		})
	}
	if len(finishEntries) == 0 {
		return out, nil
	}

	var entries []batchEntryTagged
	err = d.withClassifiedRetry(ctx, "UploadFilesBatchFinish", func() error {
		d.finishBatchMu.Lock()
		defer d.finishBatchMu.Unlock()
		var ferr error
		entries, ferr = client.uploadSessionFinishBatch(ctx, finishEntries)
		return ferr
	})
	if err != nil {
		return nil, err
	}
	if len(entries) != len(finishEntries) {
		return nil, fmt.Errorf("dropbox: finish_batch returned %d entries for %d items", len(entries), len(finishEntries))
	}
	for j, entry := range entries {
		i := finishIdx[j]
		switch strings.TrimSpace(entry.Tag) {
		case "success":
			var meta fileMetadata
			if err := json.Unmarshal(entry.Metadata, &meta); err != nil {
				out[i].Err = fmt.Errorf("dropbox: decode success metadata: %w", err)
				continue
			}
			base := basePaths[i]
			if items[i].Metadata == nil || (strings.TrimSpace(items[i].Metadata["location_path"]) == "" && strings.TrimSpace(items[i].Metadata["parent_path"]) == "") {
				base = folderBasePath(meta, base)
			}
			out[i].File = d.metaToFile(meta, base)
		default:
			out[i].Err = batchEntryFailureError(entry)
		}
	}
	if d.session.degradation != nil {
		d.session.degradation.ClearThrottleStreak()
	}
	return out, nil
}

func closeUploadBatchBodies(items []types.UploadFilesBatchItem) {
	for _, item := range items {
		if item.Body != nil {
			_ = item.Body.Close()
		}
	}
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestCreateFolderBatchSyncComplete(t *testing.T) {
	var sawPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/files/create_folder_batch") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var arg createFolderBatchArg
		if err := json.Unmarshal(raw, &arg); err != nil {
			t.Fatal(err)
		}
		sawPaths = arg.Paths
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			".tag":"complete",
			"entries":[
				{".tag":"success","metadata":{".tag":"folder","id":"id:1","name":"a","path_display":"/extra/a","path_lower":"/extra/a"}},
				{".tag":"failure","failure":{".tag":"path","path":{".tag":"conflict","conflict":{".tag":"folder"}}}}
			]
		}`))
	}))
	defer srv.Close()

	c := &Client{httpClient: &http.Client{Transport: localDropboxRPCTransport{base: srv.URL}}}
	entries, err := c.createFolderBatch(context.Background(), []string{"/extra/a", "/extra/b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sawPaths) != 2 || sawPaths[0] != "/extra/a" {
		t.Fatalf("paths=%v", sawPaths)
	}
	if len(entries) != 2 || entries[0].Tag != "success" || entries[1].Tag != "failure" {
		t.Fatalf("entries=%+v", entries)
	}
	meta, err := decodeBatchSuccessFolder(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != "id:1" || meta.Name != "a" {
		t.Fatalf("meta=%+v", meta)
	}
	failErr := batchEntryFailureError(entries[1])
	apiErr, ok := failErr.(*APIError)
	if !ok {
		t.Fatalf("want *APIError, got %T", failErr)
	}
	if apiErr.ErrorTag != "path/conflict/folder" {
		t.Fatalf("ErrorTag=%q want path/conflict/folder", apiErr.ErrorTag)
	}
	if apiErr.Status != http.StatusConflict {
		t.Fatalf("Status=%d want 409", apiErr.Status)
	}
	if !strings.Contains(apiErr.Error(), "path/conflict/folder") {
		t.Fatalf("Error()=%q", apiErr.Error())
	}
}

func TestBatchEntryFailureErrorDoesNotForce409(t *testing.T) {
	entries, err := decodeBatchEntries(json.RawMessage(`[
		{".tag":"failure","failure":{".tag":"path","path":{".tag":"not_found"}}},
		{".tag":"failure","failure":{".tag":"path","path":{".tag":"too_many_write_operations"}}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	notFound := batchEntryFailureError(entries[0]).(*APIError)
	if notFound.ErrorTag != "path/not_found" || notFound.Status != 0 {
		t.Fatalf("not_found: tag=%q status=%d", notFound.ErrorTag, notFound.Status)
	}
	writes := batchEntryFailureError(entries[1]).(*APIError)
	if writes.ErrorTag != "path/too_many_write_operations" || writes.Status != 0 {
		t.Fatalf("writes: tag=%q status=%d", writes.ErrorTag, writes.Status)
	}
}

func TestMapBatchEntryFailureWriteContentionIsRateLimited(t *testing.T) {
	d := &DropboxFS{session: &Session{degradation: types.NewFSDegradationState()}}
	entries, err := decodeBatchEntries(json.RawMessage(`[
		{".tag":"failure","failure":{".tag":"path","path":{".tag":"too_many_write_operations"}}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	mapped := d.mapBatchEntryFailure(entries[0], "CreateFolderBatch")
	var rl *credentials.RateLimitedError
	if !errors.As(mapped, &rl) || rl == nil {
		t.Fatalf("want RateLimitedError, got %T %v", mapped, mapped)
	}
	if !strings.Contains(mapped.Error(), "too_many_write_operations") {
		t.Fatalf("Error()=%q", mapped.Error())
	}
	snap := d.session.degradation.DegradationState()
	if snap.RateLimitedUntil.IsZero() {
		t.Fatal("expected degradation RateLimitedUntil after write contention")
	}
}

func TestDecodeBatchSuccessFileRejectsNestedMetadataShape(t *testing.T) {
	// Old incorrect fixture shape: nested metadata with empty top-level id/name.
	entries, err := decodeBatchEntries(json.RawMessage(`[
		{".tag":"success","metadata":{".tag":"file","id":"id:a","name":"a.txt","size":5}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeBatchSuccessFile(entries[0]); err == nil {
		t.Fatal("expected error for nested-metadata finish_batch shape")
	}
}

func TestCreateFolderBatchAsyncThenCheck(t *testing.T) {
	checks := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/files/create_folder_batch"):
			_, _ = w.Write([]byte(`{".tag":"async_job_id","async_job_id":"job-1"}`))
		case strings.HasSuffix(r.URL.Path, "/files/create_folder_batch/check"):
			checks++
			if checks == 1 {
				_, _ = w.Write([]byte(`{".tag":"in_progress"}`))
				return
			}
			_, _ = w.Write([]byte(`{
				".tag":"complete",
				"entries":[
					{".tag":"success","metadata":{".tag":"folder","id":"id:9","name":"z","path_display":"/z","path_lower":"/z"}}
				]
			}`))
		default:
			t.Fatalf("path=%s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &Client{httpClient: &http.Client{Transport: localDropboxRPCTransport{base: srv.URL}}}
	entries, err := c.createFolderBatch(context.Background(), []string{"/z"})
	if err != nil {
		t.Fatal(err)
	}
	if checks < 2 || len(entries) != 1 || entries[0].Tag != "success" {
		t.Fatalf("checks=%d entries=%+v", checks, entries)
	}
}

func TestDeleteBatchSyncComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/files/delete_batch") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			".tag":"complete",
			"entries":[
				{".tag":"success","metadata":{".tag":"file","id":"id:f","name":"a.txt"}},
				{".tag":"failure","failure":{".tag":"path_lookup","path_lookup":{".tag":"not_found"}}}
			]
		}`))
	}))
	defer srv.Close()

	c := &Client{httpClient: &http.Client{Transport: localDropboxRPCTransport{base: srv.URL}}}
	entries, err := c.deleteBatch(context.Background(), []string{"id:f", "id:missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Tag != "success" || entries[1].Tag != "failure" {
		t.Fatalf("entries=%+v", entries)
	}
	delFail := batchEntryFailureError(entries[1]).(*APIError)
	if delFail.ErrorTag != "path_lookup/not_found" || delFail.Status != 0 {
		t.Fatalf("delete fail: tag=%q status=%d", delFail.ErrorTag, delFail.Status)
	}
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
}

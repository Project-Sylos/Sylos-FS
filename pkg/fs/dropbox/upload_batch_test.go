// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// dualDropboxTransport rewrites both api and content Dropbox hosts to one httptest server.
type dualDropboxTransport struct {
	base string
}

func (t dualDropboxTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(t.base)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestUploadFilesBatchStartAppendFinish(t *testing.T) {
	var mu sync.Mutex
	sessionOffsets := map[string]uint64{}
	var finishEntries []uploadSessionFinishArg

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/files/upload_session/start_batch"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session_ids":["sess-a","sess-b"]}`))
		case strings.HasSuffix(r.URL.Path, "/files/upload_session/append_v2"):
			var arg struct {
				Cursor struct {
					SessionID string `json:"session_id"`
					Offset    uint64 `json:"offset"`
				} `json:"cursor"`
			}
			_ = json.Unmarshal([]byte(r.Header.Get("Dropbox-API-Arg")), &arg)
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			sessionOffsets[arg.Cursor.SessionID] = arg.Cursor.Offset + uint64(len(body))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/files/upload_session/finish_batch") &&
			!strings.Contains(r.URL.Path, "/check"):
			var arg uploadSessionFinishBatchArg
			_ = json.NewDecoder(r.Body).Decode(&arg)
			mu.Lock()
			finishEntries = arg.Entries
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				".tag":"complete",
				"entries":[
					{".tag":"success","metadata":{".tag":"file","id":"id:a","name":"a.txt","path_display":"/a.txt","path_lower":"/a.txt","size":5}},
					{".tag":"success","metadata":{".tag":"file","id":"id:b","name":"b.txt","path_display":"/b.txt","path_lower":"/b.txt","size":3}}
				]
			}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &Client{httpClient: &http.Client{Transport: dualDropboxTransport{base: srv.URL}}}
	ids, err := c.uploadSessionStartBatch(context.Background(), 2)
	if err != nil {
		t.Fatalf("start_batch: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids=%v", ids)
	}
	offA, err := c.appendUploadSession(context.Background(), ids[0], strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("append a: %v", err)
	}
	offB, err := c.appendUploadSession(context.Background(), ids[1], strings.NewReader("bye"))
	if err != nil {
		t.Fatalf("append b: %v", err)
	}
	if offA != 5 || offB != 3 {
		t.Fatalf("offsets a=%d b=%d", offA, offB)
	}
	entries, err := c.uploadSessionFinishBatch(context.Background(), []uploadSessionFinishArg{
		{Cursor: uploadSessionCursor{SessionID: ids[0], Offset: offA}, Commit: uploadArg{Path: "/a.txt", Mode: "overwrite"}},
		{Cursor: uploadSessionCursor{SessionID: ids[1], Offset: offB}, Commit: uploadArg{Path: "/b.txt", Mode: "overwrite"}},
	})
	if err != nil {
		t.Fatalf("finish_batch: %v", err)
	}
	if len(entries) != 2 || entries[0].Tag != "success" || entries[1].Tag != "success" {
		t.Fatalf("entries=%+v", entries)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(finishEntries) != 2 {
		t.Fatalf("finish entries=%d", len(finishEntries))
	}
	if finishEntries[0].Cursor.SessionID != "sess-a" || finishEntries[0].Cursor.Offset != 5 {
		t.Fatalf("entry0=%+v", finishEntries[0])
	}
	if finishEntries[1].Cursor.SessionID != "sess-b" || finishEntries[1].Cursor.Offset != 3 {
		t.Fatalf("entry1=%+v", finishEntries[1])
	}
	if sessionOffsets["sess-a"] != 5 || sessionOffsets["sess-b"] != 3 {
		t.Fatalf("sessionOffsets=%v (no cross-session bleed expected)", sessionOffsets)
	}
}

func TestUploadSessionAppendEmptyClosesAtZero(t *testing.T) {
	var closeAtZero bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/files/upload_session/append_v2") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		arg := r.Header.Get("Dropbox-API-Arg")
		if strings.Contains(arg, `"offset":0`) && strings.Contains(arg, `"close":true`) {
			closeAtZero = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := &Client{httpClient: &http.Client{Transport: localDropboxContentTransport{base: srv.URL}}}
	off, err := c.appendUploadSession(context.Background(), "sess-empty", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if off != 0 || !closeAtZero {
		t.Fatalf("off=%d closeAtZero=%v", off, closeAtZero)
	}
}

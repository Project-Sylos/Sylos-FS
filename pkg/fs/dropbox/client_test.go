// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRPCParameterlessEndpointUsesNullJSONBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		if !strings.HasSuffix(r.URL.Path, "/users/get_current_account") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":{"display_name":"Test"},"root_info":{".tag":"user","root_namespace_id":"ns1","home_namespace_id":"ns1"}}`))
	}))
	defer srv.Close()

	c := &Client{
		httpClient: &http.Client{
			Transport: localDropboxRPCTransport{base: srv.URL},
		},
	}
	var acct currentAccount
	if err := c.rpc(context.Background(), "users/get_current_account", nil, &acct); err != nil {
		t.Fatalf("rpc: %v", err)
	}
	if gotBody != "null" {
		t.Fatalf("body=%q want null", gotBody)
	}
	if acct.RootInfo.HomeNamespaceID != "ns1" {
		t.Fatalf("home ns=%q", acct.RootInfo.HomeNamespaceID)
	}
}

// localDropboxRPCTransport rewrites api.dropboxapi.com RPC calls to a httptest server.
type localDropboxRPCTransport struct {
	base string
}

func (t localDropboxRPCTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(t.base)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestUploadSessionFinishUsesContentHost(t *testing.T) {
	var finishHost string
	var sawAppendClose bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/files/upload_session/start"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session_id":"sess-1"}`))
		case strings.HasSuffix(r.URL.Path, "/files/upload_session/append_v2"):
			arg := r.Header.Get("Dropbox-API-Arg")
			if strings.Contains(arg, `"close":true`) {
				sawAppendClose = true
			}
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/files/upload_session/finish"):
			finishHost = r.Host
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"metadata":{".tag":"file","id":"id:1","name":"a.txt","path_display":"/a.txt","path_lower":"/a.txt","size":0}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &Client{
		httpClient: &http.Client{
			Transport: localDropboxContentTransport{base: srv.URL},
		},
	}
	_, err := c.uploadSession(context.Background(), "/a.txt", strings.NewReader(""))
	if err != nil {
		t.Fatalf("uploadSession: %v", err)
	}
	if !sawAppendClose {
		t.Fatal("expected empty-body append_v2 with close=true")
	}
	if finishHost == "" {
		t.Fatal("finish request was not observed")
	}
}

// localDropboxContentTransport rewrites content.dropboxapi.com calls to a httptest server.
type localDropboxContentTransport struct {
	base string
}

func (t localDropboxContentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(t.base)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

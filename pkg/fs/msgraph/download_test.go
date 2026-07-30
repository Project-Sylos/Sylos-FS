// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// authAlwaysTransport mimics oauth2.Transport: every RoundTrip gets Authorization,
// including redirected hops. Graph CDNs reject that with 401 Unauthenticated.
type authAlwaysTransport struct {
	base http.RoundTripper
}

func (t authAlwaysTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer test-token")
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req2)
}

type rewriteHostTransport struct {
	host string
	base http.RoundTripper
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := http.NewRequestWithContext(req.Context(), req.Method, t.host+req.URL.RequestURI(), req.Body)
	if err != nil {
		return nil, err
	}
	u.Header = req.Clone(req.Context()).Header
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(u)
}

func TestOpenDownloadViaClientContentRedirect(t *testing.T) {
	t.Parallel()
	var cdnGotAuth bool
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			cdnGotAuth = true
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"Unauthenticated"}}`))
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer cdn.Close()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.Contains(r.URL.RawQuery, "downloadUrl"):
			// No downloadUrl in body → OpenDownload falls back to /content.
			_, _ = w.Write([]byte(`{"id":"1","name":"f.txt","size":2}`))
		case strings.HasSuffix(path, "/content"):
			http.Redirect(w, r, cdn.URL+"/b", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	})

	rt := authAlwaysTransport{base: rewriteHostTransport{host: srv.URL, base: http.DefaultTransport}}
	client := NewClient(&http.Client{Transport: rt})

	rc, err := client.OpenDownload(context.Background(), "/me/drive/items/1")
	if err != nil {
		t.Fatalf("OpenDownload: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "ok" {
		t.Fatalf("body=%q", got)
	}
	if cdnGotAuth {
		t.Fatal("Authorization leaked to CDN")
	}
}

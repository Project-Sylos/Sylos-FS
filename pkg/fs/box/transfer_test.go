// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package box

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/fs/optimeout"
)

func TestIsTransientUploadErr(t *testing.T) {
	ctx := context.Background()
	if !isTransientUploadErr(ctx, context.DeadlineExceeded) {
		t.Fatal("deadline")
	}
	if isTransientUploadErr(ctx, context.Canceled) {
		t.Fatal("canceled")
	}
	if !isTransientUploadErr(ctx, &APIError{Status: http.StatusBadGateway}) {
		t.Fatal("502")
	}
	if isTransientUploadErr(ctx, &APIError{Status: http.StatusBadRequest, Code: "invalid_part_size"}) {
		t.Fatal("400")
	}
}

func TestUploadSessionPartWithRetryExhausts(t *testing.T) {
	srvCalls := 0
	c := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		srvCalls++
		return nil, context.DeadlineExceeded
	})}}
	_, err := c.uploadSessionPartWithRetry(context.Background(), "http://example.invalid/part", make([]byte, 64), 0, 64)
	if err == nil {
		t.Fatal("expected error")
	}
	if srvCalls != optimeout.ChunkAttempts {
		t.Fatalf("attempts=%d want %d", srvCalls, optimeout.ChunkAttempts)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wrap: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

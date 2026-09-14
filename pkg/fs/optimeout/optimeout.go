// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package optimeout provides per-RPC and per-chunk deadlines for FS adapters.
// Chunked uploads use a fresh size-scaled deadline per part; metadata RPCs use a
// fixed RPC budget. Do not wrap an entire multi-chunk upload or multi-page list
// in one overall timeout.
package optimeout

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	// MinChunkBytesPerSec floors chunk deadline scaling (1 MiB/s).
	MinChunkBytesPerSec = 1 << 20
	MinChunk            = 30 * time.Second
	MaxChunk            = 5 * time.Minute
	ChunkAttempts       = 3
	// Commit covers session finish / assemble (small request, server may wait).
	Commit         = 5 * time.Minute
	CommitAttempts = 2
	// RPC covers ListChildren, Delete, CreateFolder, session start, and similar.
	RPC = 60 * time.Second
	// Abort is best-effort cleanup of an upload session.
	Abort = 30 * time.Second

	// IORead is the per-Read deadline for copy-loop / ctxstream reads (resets each call).
	IORead = MinChunk
	// IOWrite is the per-Write deadline; may wait on a full cloud part PUT behind a pipe.
	IOWrite = MaxChunk
	// IOClose is the per-Close deadline for stream finish when not using a longer ME commit window.
	IOClose = Commit
)

// Chunk returns a size-scaled part PUT/append deadline clamped to [MinChunk, MaxChunk].
func Chunk(partBytes int64) time.Duration {
	if partBytes <= 0 {
		return MinChunk
	}
	maxSecs := int64(MaxChunk / time.Second)
	secs := partBytes / MinChunkBytesPerSec
	if secs > maxSecs {
		return MaxChunk
	}
	d := time.Duration(secs) * time.Second
	if d < MinChunk {
		return MinChunk
	}
	return d
}

// Bound attaches a deadline; caller must cancel.
func Bound(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

// IsTransient reports network/deadline failures safe to retry for the same chunk
// when the parent context is still live. Provider HTTP status checks should layer
// on top (429/5xx).
func IsTransient(parent context.Context, err error) bool {
	if err == nil || parent.Err() != nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}

// SoftHTTP is true for throttle and server errors that warrant another chunk attempt.
func SoftHTTP(status int) bool {
	switch status {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// Do runs fn under a fresh timeout up to attempts times on transient errors.
func Do[T any](ctx context.Context, timeout time.Duration, attempts int, fn func(context.Context) (T, error)) (T, error) {
	return DoIf(ctx, timeout, attempts, IsTransient, fn)
}

// DoIf is Do with a custom transient predicate (providers add 429/5xx).
func DoIf[T any](ctx context.Context, timeout time.Duration, attempts int, transient func(context.Context, error) bool, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if attempts < 1 {
		attempts = 1
	}
	if transient == nil {
		transient = IsTransient
	}
	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}
		cctx, cancel := Bound(ctx, timeout)
		v, err := fn(cctx)
		cancel()
		if err == nil {
			return v, nil
		}
		last = err
		if !transient(ctx, err) {
			return zero, err
		}
	}
	return zero, fmt.Errorf("fs op failed after %d attempts: %w", attempts, last)
}

// DoChunk is Do with Chunk(partBytes) and ChunkAttempts.
func DoChunk[T any](ctx context.Context, partBytes int64, fn func(context.Context) (T, error)) (T, error) {
	return Do(ctx, Chunk(partBytes), ChunkAttempts, fn)
}

// DoChunkIf is DoChunk with a custom transient predicate.
func DoChunkIf[T any](ctx context.Context, partBytes int64, transient func(context.Context, error) bool, fn func(context.Context) (T, error)) (T, error) {
	return DoIf(ctx, Chunk(partBytes), ChunkAttempts, transient, fn)
}

// DoCommit is Do with Commit and CommitAttempts.
func DoCommit[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	return Do(ctx, Commit, CommitAttempts, fn)
}

// DoCommitIf is DoCommit with a custom transient predicate.
func DoCommitIf[T any](ctx context.Context, transient func(context.Context, error) bool, fn func(context.Context) (T, error)) (T, error) {
	return DoIf(ctx, Commit, CommitAttempts, transient, fn)
}

// DoRPC is a single attempt under RPC (classified retry owns outer retries).
func DoRPC[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	return Do(ctx, RPC, 1, fn)
}

// PerRequest wraps base so each RoundTrip gets a fresh deadline. Cancel runs when
// the response body is closed (or immediately on RoundTrip error).
func PerRequest(base http.RoundTripper, d time.Duration) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return perRequestRT{base: base, d: d}
}

type perRequestRT struct {
	base http.RoundTripper
	d    time.Duration
}

func (t perRequestRT) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := Bound(req.Context(), t.d)
	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

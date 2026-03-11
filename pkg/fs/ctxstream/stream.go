// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package ctxstream provides context-aware io.Reader/io.Writer wrappers so
// migration workers can abort stalled transfers when ctx is cancelled.
package ctxstream

import (
	"context"
	"io"
)

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
		return c.r.Read(p)
	}
}

type ctxWriter struct {
	ctx context.Context
	w   io.Writer
}

func (c *ctxWriter) Write(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
		return c.w.Write(p)
	}
}

type readCloser struct {
	io.Reader
	io.Closer
}

type writeCloser struct {
	io.Writer
	io.Closer
}

// NewReader wraps r so each Read checks ctx.Done() first (no Close; caller closes underlying if needed).
func NewReader(ctx context.Context, r io.Reader) io.Reader {
	return &ctxReader{ctx: ctx, r: r}
}

// NewReadCloser wraps rc so each Read checks ctx.Done() first.
func NewReadCloser(ctx context.Context, rc io.ReadCloser) io.ReadCloser {
	return &readCloser{
		Reader: &ctxReader{ctx: ctx, r: rc},
		Closer: rc,
	}
}

// NewWriteCloser wraps wc so each Write checks ctx.Done() first.
func NewWriteCloser(ctx context.Context, wc io.WriteCloser) io.WriteCloser {
	return &writeCloser{
		Writer: &ctxWriter{ctx: ctx, w: wc},
		Closer: wc,
	}
}

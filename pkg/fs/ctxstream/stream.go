// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package ctxstream provides context-aware io.Reader/io.Writer wrappers so
// migration workers can abort stalled transfers when ctx is cancelled or a
// per-call IO deadline elapses.
package ctxstream

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/fs/optimeout"
)

type ctxReader struct {
	ctx     context.Context
	r       io.Reader
	timeout time.Duration
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	if c.timeout <= 0 {
		return c.r.Read(p)
	}
	// Private buffer so a timed-out Read goroutine cannot race the caller's p.
	tmp := readBufPool.Get().(*[]byte)
	if cap(*tmp) < len(p) {
		*tmp = make([]byte, len(p))
	}
	buf := (*tmp)[:len(p)]
	type res struct {
		n   int
		err error
	}
	done := make(chan res, 1)
	go func() {
		n, err := c.r.Read(buf)
		done <- res{n: n, err: err}
	}()
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		// Abandon tmp; the Read goroutine may still write to it.
		return 0, c.ctx.Err()
	case <-timer.C:
		return 0, fmt.Errorf("read timed out after %s: %w", c.timeout, context.DeadlineExceeded)
	case r := <-done:
		n := copy(p, buf[:r.n])
		readBufPool.Put(tmp)
		return n, r.err
	}
}

var readBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 64*1024)
		return &b
	},
}

type ctxWriter struct {
	ctx     context.Context
	w       io.Writer
	timeout time.Duration
}

func (c *ctxWriter) Write(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	if c.timeout <= 0 {
		return c.w.Write(p)
	}
	// Copy payload so a timed-out Write goroutine does not race caller reuse of p.
	payload := append([]byte(nil), p...)
	type res struct {
		n   int
		err error
	}
	done := make(chan res, 1)
	go func() {
		n, err := c.w.Write(payload)
		done <- res{n: n, err: err}
	}()
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	case <-timer.C:
		return 0, fmt.Errorf("write timed out after %s: %w", c.timeout, context.DeadlineExceeded)
	case r := <-done:
		return r.n, r.err
	}
}

type timedCloser struct {
	c       io.Closer
	ctx     context.Context
	timeout time.Duration
}

func (c *timedCloser) Close() error {
	if c.c == nil {
		return nil
	}
	if c.timeout <= 0 {
		return c.c.Close()
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- c.c.Close() }()
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-timer.C:
		return fmt.Errorf("close timed out after %s: %w", c.timeout, context.DeadlineExceeded)
	case err := <-done:
		return err
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

// NewReader wraps r so each Read checks ctx and applies the default IO read deadline.
func NewReader(ctx context.Context, r io.Reader) io.Reader {
	return NewReaderTimeout(ctx, r, optimeout.IORead)
}

// NewReaderTimeout wraps r with ctx checks and a per-Read deadline.
func NewReaderTimeout(ctx context.Context, r io.Reader, timeout time.Duration) io.Reader {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ctxReader{ctx: ctx, r: r, timeout: timeout}
}

// NewReadCloser wraps rc so each Read checks ctx and applies the default IO read deadline.
func NewReadCloser(ctx context.Context, rc io.ReadCloser) io.ReadCloser {
	return NewReadCloserTimeout(ctx, rc, optimeout.IORead, optimeout.IOClose)
}

// NewReadCloserTimeout wraps rc with per-Read and Close deadlines.
func NewReadCloserTimeout(ctx context.Context, rc io.ReadCloser, readTimeout, closeTimeout time.Duration) io.ReadCloser {
	if ctx == nil {
		ctx = context.Background()
	}
	return &readCloser{
		Reader: &ctxReader{ctx: ctx, r: rc, timeout: readTimeout},
		Closer: &timedCloser{c: rc, ctx: ctx, timeout: closeTimeout},
	}
}

// NewWriteCloser wraps wc so each Write checks ctx and applies the default IO write deadline.
func NewWriteCloser(ctx context.Context, wc io.WriteCloser) io.WriteCloser {
	return NewWriteCloserTimeout(ctx, wc, optimeout.IOWrite, optimeout.IOClose)
}

// NewWriteCloserTimeout wraps wc with per-Write and Close deadlines.
func NewWriteCloserTimeout(ctx context.Context, wc io.WriteCloser, writeTimeout, closeTimeout time.Duration) io.WriteCloser {
	if ctx == nil {
		ctx = context.Background()
	}
	return &writeCloser{
		Writer: &ctxWriter{ctx: ctx, w: wc, timeout: writeTimeout},
		Closer: &timedCloser{c: wc, ctx: ctx, timeout: closeTimeout},
	}
}

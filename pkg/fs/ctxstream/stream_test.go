// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package ctxstream

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

type stallReader struct {
	unblock <-chan struct{}
}

func (s stallReader) Read(p []byte) (int, error) {
	<-s.unblock
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = 'x'
	return 1, nil
}

func TestReaderTimesOut(t *testing.T) {
	unblock := make(chan struct{})
	r := NewReaderTimeout(context.Background(), stallReader{unblock: unblock}, 20*time.Millisecond)
	_, err := r.Read(make([]byte, 8))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	close(unblock)
}

type stallWriter struct {
	unblock <-chan struct{}
}

func (s stallWriter) Write(p []byte) (int, error) {
	<-s.unblock
	return len(p), nil
}

func (s stallWriter) Close() error {
	<-s.unblock
	return nil
}

func TestWriteCloserTimesOut(t *testing.T) {
	unblock := make(chan struct{})
	w := NewWriteCloserTimeout(context.Background(), stallWriter{unblock: unblock}, 20*time.Millisecond, 20*time.Millisecond)
	_, err := w.Write([]byte("hi"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("write: %v", err)
	}
	err = w.Close()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close: %v", err)
	}
	close(unblock)
}

func TestReaderRespectsParentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NewReaderTimeout(ctx, stallReader{unblock: make(chan struct{})}, time.Minute)
	_, err := r.Read(make([]byte, 4))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestReaderPassesThrough(t *testing.T) {
	r := NewReaderTimeout(context.Background(), &immediateReader{b: []byte("ab")}, time.Second)
	buf := make([]byte, 2)
	n, err := r.Read(buf)
	if err != nil || n != 2 || string(buf) != "ab" {
		t.Fatalf("n=%d err=%v buf=%q", n, err, buf)
	}
}

type immediateReader struct{ b []byte }

func (r *immediateReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

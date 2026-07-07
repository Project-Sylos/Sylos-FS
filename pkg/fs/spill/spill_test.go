// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package spill

import (
	"io"
	"testing"
)

func TestWriterMemoryOnly(t *testing.T) {
	w := NewWriter(1024)
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	ra, n, err := w.ReaderAt()
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("size=%d want 5", n)
	}
	buf := make([]byte, 5)
	if _, err := ra.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		t.Fatalf("got %q", buf)
	}
}

func TestWriterSpillsToTemp(t *testing.T) {
	w := NewWriter(16)
	data := make([]byte, 32)
	for i := range data {
		data[i] = byte(i)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if w.Size() != int64(len(data)) {
		t.Fatalf("size=%d want %d", w.Size(), len(data))
	}
	ra, n, err := w.ReaderAt()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 32)
	if _, err := io.ReadFull(&readerAtReader{ra: ra, size: n}, got); err != nil {
		t.Fatal(err)
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("byte %d: got %d want %d", i, got[i], data[i])
		}
	}
}

type readerAtReader struct {
	ra   io.ReaderAt
	size int64
	off  int64
}

func (r *readerAtReader) Read(p []byte) (int, error) {
	if r.off >= r.size {
		return 0, io.EOF
	}
	n, err := r.ra.ReadAt(p, r.off)
	r.off += int64(n)
	if r.off >= r.size {
		return n, io.EOF
	}
	return n, err
}

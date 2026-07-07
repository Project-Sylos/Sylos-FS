// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package spill provides a WriteCloser that buffers up to a memory threshold then
// spills to a temp file so adapters can upload via io.ReaderAt without loading
// entire files into RAM.
package spill

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// DefaultMemoryThreshold is the in-memory buffer size before spilling to disk.
const DefaultMemoryThreshold = 8 * 1024 * 1024

// Writer accumulates writes in memory and spills to a temp file when the threshold is exceeded.
type Writer struct {
	buf       bytes.Buffer
	tmp       *os.File
	tmpPath   string
	threshold int
	closed    bool
}

// NewWriter returns a spill writer. threshold <= 0 uses DefaultMemoryThreshold.
func NewWriter(threshold int) *Writer {
	if threshold <= 0 {
		threshold = DefaultMemoryThreshold
	}
	return &Writer{threshold: threshold}
}

// Write appends data, spilling to a temp file when the memory threshold is exceeded.
func (w *Writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("spill: write on closed writer")
	}
	if w.tmp != nil {
		return w.tmp.Write(p)
	}
	if w.buf.Len()+len(p) > w.threshold {
		if err := w.spillToTemp(); err != nil {
			return 0, err
		}
		return w.tmp.Write(p)
	}
	return w.buf.Write(p)
}

func (w *Writer) spillToTemp() error {
	tmp, err := os.CreateTemp("", "sylos-spill-*")
	if err != nil {
		return fmt.Errorf("spill: create temp file: %w", err)
	}
	if w.buf.Len() > 0 {
		if _, err := tmp.Write(w.buf.Bytes()); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return fmt.Errorf("spill: write buffer to temp: %w", err)
		}
		w.buf.Reset()
	}
	w.tmp = tmp
	w.tmpPath = tmp.Name()
	return nil
}

// Size returns the number of bytes written so far.
func (w *Writer) Size() int64 {
	if w.tmp != nil {
		fi, err := w.tmp.Stat()
		if err != nil {
			return 0
		}
		return fi.Size()
	}
	return int64(w.buf.Len())
}

// ReaderAt returns a reader and size for upload. The caller must not write after this call.
func (w *Writer) ReaderAt() (io.ReaderAt, int64, error) {
	if w.tmp != nil {
		if _, err := w.tmp.Seek(0, io.SeekStart); err != nil {
			return nil, 0, fmt.Errorf("spill: seek temp file: %w", err)
		}
		fi, err := w.tmp.Stat()
		if err != nil {
			return nil, 0, err
		}
		return w.tmp, fi.Size(), nil
	}
	b := w.buf.Bytes()
	return bytes.NewReader(b), int64(len(b)), nil
}

// Close releases resources. Call after the upload completes.
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.tmp != nil {
		path := w.tmpPath
		err := w.tmp.Close()
		w.tmp = nil
		if path != "" {
			_ = os.Remove(path)
		}
		w.tmpPath = ""
		return err
	}
	return nil
}

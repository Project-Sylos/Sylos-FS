// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"context"
	"io"
	"os"

	"codeberg.org/Sylos/Sylos-FS/pkg/fs/ctxstream"
)

// fadviseReadCloser provides ctx-aware Read then FADV_DONTNEED + single Close on the file.
type fadviseReadCloser struct {
	io.Reader
	file *os.File
}

func newFadviseReadCloser(ctx context.Context, file *os.File) io.ReadCloser {
	return &fadviseReadCloser{
		Reader: ctxstream.NewReader(ctx, file),
		file:   file,
	}
}

func (f *fadviseReadCloser) Close() error {
	if f.file == nil {
		return nil
	}
	_ = fadviseDontNeed(f.file)
	err := f.file.Close()
	f.file = nil
	return err
}

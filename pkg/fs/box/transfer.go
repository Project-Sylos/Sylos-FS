// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package box

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"codeberg.org/Sylos/Sylos-FS/pkg/fs/spill"
)

const pendingFilePrefix = "pending:"

func pendingFileID(parentID, name string) string {
	return fmt.Sprintf("%s%s:%s", pendingFilePrefix, parentID, name)
}

func parsePendingFileID(fileID string) (parentID, name string, ok bool) {
	if !strings.HasPrefix(fileID, pendingFilePrefix) {
		return "", "", false
	}
	parts := strings.SplitN(fileID, ":", 3)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

type boxWriter struct {
	bfs      *BoxFS
	ctx      context.Context
	fileID   string
	parentID string
	name     string
	create   bool

	mu     sync.Mutex
	closed bool
	spill  *spill.Writer
}

func newBoxWriter(d *BoxFS, ctx context.Context, fileID string) (*boxWriter, error) {
	w := &boxWriter{
		bfs:    d,
		ctx:    ctx,
		fileID: fileID,
		spill:  spill.NewWriter(0),
	}
	if parent, name, ok := parsePendingFileID(fileID); ok {
		w.create = true
		w.parentID = parent
		w.name = name
	}
	return w, nil
}

func (w *boxWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, fmt.Errorf("box: writer closed")
	}
	return w.spill.Write(p)
}

func (w *boxWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()

	size := w.spill.Size()
	reader, size2, err := w.spill.ReaderAt()
	if err != nil {
		_ = w.spill.Close()
		return err
	}
	if size2 >= 0 {
		size = size2
	}
	defer w.spill.Close()

	op := "UploadFile"
	if w.create {
		op = "CreateFileUpload"
	}
	return w.bfs.withClassifiedRetry(w.ctx, op, func() error {
		client, err := w.bfs.client(w.ctx)
		if err != nil {
			return err
		}
		section := io.NewSectionReader(reader, 0, size)
		if size <= SimpleUploadMaxBytes {
			if w.create {
				_, err = client.UploadNewFile(w.ctx, w.parentID, w.name, section, size)
				return err
			}
			_, err = client.UploadFileVersion(w.ctx, w.fileID, section, size)
			return err
		}
		if w.create {
			_, err = client.UploadChunked(w.ctx, w.parentID, w.name, "", reader, size)
			return err
		}
		_, err = client.UploadChunked(w.ctx, "", "", w.fileID, reader, size)
		return err
	})
}

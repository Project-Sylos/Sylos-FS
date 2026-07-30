// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Writer streams file bytes to Microsoft Graph: full upload fragments are sent
// during Write (no temp files, no full-file staging). Close finalizes the last
// fragment or a small simple PUT when the whole object fits in SimpleUploadMaxBytes.
type Writer struct {
	ops    *AdapterOps
	ctx    context.Context
	fileID string
	parent string
	name   string
	create bool
	// declaredSize is the CreateFile size when known; -1 if unknown.
	declaredSize int64

	mu             sync.Mutex
	closed         bool
	buf            []byte
	offset         int64
	uploadURL      string
	sessionStarted bool
	simpleOnly     bool // declared size fits simple PUT; buffer until Close
}

// NewWriter creates a streaming Graph upload writer.
func NewWriter(ops *AdapterOps, ctx context.Context, fileID string) (*Writer, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	w := &Writer{
		ops:          ops,
		ctx:          ctx,
		fileID:       fileID,
		declaredSize: -1,
	}
	if parent, name, size, ok := ParsePendingFileID(fileID); ok {
		w.create = true
		w.parent = parent
		w.name = name
		w.declaredSize = size
		if size >= 0 && size <= SimpleUploadMaxBytes {
			w.simpleOnly = true
		}
	}
	return w, nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, fmt.Errorf("msgraph: writer closed")
	}
	if len(p) == 0 {
		return 0, nil
	}

	if w.simpleOnly {
		w.buf = append(w.buf, p...)
		if int64(len(w.buf)) > SimpleUploadMaxBytes {
			return 0, fmt.Errorf("msgraph: simple upload exceeded %d bytes", SimpleUploadMaxBytes)
		}
		return len(p), nil
	}

	// Unknown size: stay on simple path until we would exceed the simple limit, then
	// promote to an upload session and flush.
	if !w.sessionStarted && w.declaredSize < 0 {
		if int64(len(w.buf)+len(p)) <= SimpleUploadMaxBytes {
			w.buf = append(w.buf, p...)
			return len(p), nil
		}
		w.buf = append(w.buf, p...)
		if err := w.flushSessionChunksLocked(false); err != nil {
			return 0, err
		}
		return len(p), nil
	}

	w.buf = append(w.buf, p...)
	if err := w.flushSessionChunksLocked(false); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true

	if w.simpleOnly || (!w.sessionStarted && w.declaredSize < 0 && int64(len(w.buf)) <= SimpleUploadMaxBytes) {
		return w.simplePutLocked()
	}
	return w.flushSessionChunksLocked(true)
}

func (w *Writer) simplePutLocked() error {
	data := w.buf
	w.buf = nil
	size := int64(len(data))
	op := "UploadFile"
	if w.create {
		op = "CreateFileUpload"
	}
	return w.ops.WithClassifiedRetry(w.ctx, op, func() error {
		client, err := w.ops.Client(w.ctx)
		if err != nil {
			return err
		}
		driveID := w.ops.EffectiveDriveID()
		var contentPath string
		if w.create {
			contentPath = PutContentByPath(driveID, w.parent, w.name)
		} else {
			contentPath = ContentPath(driveID, w.fileID)
		}
		_, err = client.PutContent(w.ctx, contentPath, bytes.NewReader(data), size)
		return err
	})
}

// flushSessionChunksLocked uploads complete UploadChunkSize fragments. When final is true,
// also uploads any trailing bytes and uses the definitive total size in Content-Range.
func (w *Writer) flushSessionChunksLocked(final bool) error {
	for {
		need := UploadChunkSize
		if final {
			if len(w.buf) == 0 {
				if !w.sessionStarted && w.offset == 0 {
					// Empty file.
					return w.simplePutLocked()
				}
				return nil
			}
			need = len(w.buf)
		} else if len(w.buf) < UploadChunkSize {
			return nil
		}

		if err := w.ensureSessionLocked(); err != nil {
			return err
		}

		n := need
		if n > len(w.buf) {
			n = len(w.buf)
		}
		chunk := append([]byte(nil), w.buf[:n]...)
		w.buf = append([]byte(nil), w.buf[n:]...)

		total := w.declaredSize
		if final && len(w.buf) == 0 {
			total = w.offset + int64(len(chunk))
		} else if total < 0 {
			// Intermediate fragment with unknown total.
			total = -1
		}

		item, done, err := w.putFragmentLocked(chunk, total)
		if err != nil {
			return err
		}
		w.offset += int64(len(chunk))
		_ = item
		if done {
			w.buf = nil
			return nil
		}
		if final && len(w.buf) == 0 {
			return nil
		}
		if !final {
			continue
		}
	}
}

func (w *Writer) ensureSessionLocked() error {
	if w.sessionStarted {
		return nil
	}
	op := "UploadFile"
	if w.create {
		op = "CreateFileUpload"
	}
	var uploadURL string
	err := w.ops.WithClassifiedRetry(w.ctx, op, func() error {
		client, err := w.ops.Client(w.ctx)
		if err != nil {
			return err
		}
		driveID := w.ops.EffectiveDriveID()
		var sessionPath string
		var body any
		if w.create {
			sessionPath = CreateUploadSessionByPath(driveID, w.parent, w.name)
			body = map[string]any{
				"item": map[string]any{
					"@microsoft.graph.conflictBehavior": "replace",
					"name":                              w.name,
				},
			}
		} else {
			sessionPath = CreateUploadSessionPath(driveID, w.fileID)
			body = map[string]any{
				"item": map[string]any{
					"@microsoft.graph.conflictBehavior": "replace",
				},
			}
		}
		url, err := client.CreateUploadSession(w.ctx, sessionPath, body)
		if err != nil {
			return err
		}
		uploadURL = url
		return nil
	})
	if err != nil {
		return err
	}
	w.uploadURL = uploadURL
	w.sessionStarted = true
	return nil
}

func (w *Writer) putFragmentLocked(chunk []byte, total int64) (DriveItem, bool, error) {
	op := "UploadFile"
	if w.create {
		op = "CreateFileUpload"
	}
	var item DriveItem
	var done bool
	err := w.ops.WithClassifiedRetry(w.ctx, op, func() error {
		client, err := w.ops.Client(w.ctx)
		if err != nil {
			return err
		}
		it, complete, putErr := client.PutUploadFragment(w.ctx, w.uploadURL, chunk, w.offset, total)
		if putErr != nil {
			return putErr
		}
		item = it
		done = complete
		return nil
	})
	return item, done, err
}

// PendingFileID encodes a create-pending upload target. size may be -1 when unknown.
func PendingFileID(parentID, name string, size int64) string {
	if parentID == "" {
		parentID = "root"
	}
	return fmt.Sprintf("%s%s:%d:%s", pendingFilePrefix, parentID, size, name)
}

// ParsePendingFileID parses pending:<parent>:<size>:<name>.
// Also accepts legacy pending:<parent>:<name> (size = -1).
func ParsePendingFileID(fileID string) (parentID, name string, size int64, ok bool) {
	if !strings.HasPrefix(fileID, pendingFilePrefix) {
		return "", "", 0, false
	}
	rest := strings.TrimPrefix(fileID, pendingFilePrefix)
	parent, rest, found := strings.Cut(rest, ":")
	if !found || parent == "" || rest == "" {
		return "", "", 0, false
	}
	// New format: size:name
	sizeStr, namePart, found := strings.Cut(rest, ":")
	if found {
		if n, err := strconv.ParseInt(sizeStr, 10, 64); err == nil && namePart != "" {
			return parent, namePart, n, true
		}
	}
	// Legacy: pending:<parent>:<name> (name may contain colons via rest)
	return parent, rest, -1, true
}

var _ io.WriteCloser = (*Writer)(nil)

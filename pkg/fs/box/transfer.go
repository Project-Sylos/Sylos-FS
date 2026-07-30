// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package box

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"codeberg.org/Sylos/Sylos-FS/pkg/fs/ctxstream"
)

const pendingFilePrefix = "pending:"

func pendingFileID(parentID, name string, size int64) string {
	return fmt.Sprintf("%s%s:%d:%s", pendingFilePrefix, parentID, size, name)
}

func parsePendingFileID(fileID string) (parentID, name string, size int64, ok bool) {
	if !strings.HasPrefix(fileID, pendingFilePrefix) {
		return "", "", 0, false
	}
	rest := strings.TrimPrefix(fileID, pendingFilePrefix)
	parent, rest, found := strings.Cut(rest, ":")
	if !found || parent == "" || rest == "" {
		return "", "", 0, false
	}
	sizeStr, namePart, found := strings.Cut(rest, ":")
	if found {
		if n, err := strconv.ParseInt(sizeStr, 10, 64); err == nil && namePart != "" {
			return parent, namePart, n, true
		}
	}
	// Legacy pending:<parent>:<name>
	return parent, rest, -1, true
}

// boxWriter streams bytes to Box. Small files use multipart over a pipe; large
// files create an upload session and PUT parts as the pipe is read — no spill/temp.
type boxWriter struct {
	bfs      *BoxFS
	ctx      context.Context
	fileID   string
	parentID string
	name     string
	create   bool
	size     int64 // declared CreateFile size; -1 unknown

	mu            sync.Mutex
	closed        bool
	uploadStarted bool
	out           io.WriteCloser
	uploadDone    chan error
}

func newBoxWriter(d *BoxFS, ctx context.Context, fileID string) (*boxWriter, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	w := &boxWriter{
		bfs:    d,
		ctx:    ctx,
		fileID: fileID,
		size:   -1,
	}
	if parent, name, size, ok := parsePendingFileID(fileID); ok {
		w.create = true
		w.parentID = parent
		w.name = name
		w.size = size
	}
	return w, nil
}

func (w *boxWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return 0, fmt.Errorf("box: writer closed")
	}
	if !w.uploadStarted {
		if err := w.startUploadLocked(); err != nil {
			w.mu.Unlock()
			return 0, err
		}
	}
	out := w.out
	w.mu.Unlock()
	return out.Write(p)
}

func (w *boxWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	if !w.uploadStarted {
		w.mu.Unlock()
		return w.uploadEmpty()
	}
	out := w.out
	done := w.uploadDone
	w.mu.Unlock()

	if err := out.Close(); err != nil {
		return err
	}
	return <-done
}

func (w *boxWriter) startUploadLocked() error {
	if err := w.resolveSizeLocked(); err != nil {
		return err
	}
	pr, pw := io.Pipe()
	w.out = ctxstream.NewWriteCloser(w.ctx, pw)
	w.uploadDone = make(chan error, 1)
	w.uploadStarted = true
	body := ctxstream.NewReader(w.ctx, pr)
	go func() {
		w.uploadDone <- w.streamUpload(body)
	}()
	return nil
}

func (w *boxWriter) resolveSizeLocked() error {
	if w.size >= 0 {
		return nil
	}
	if w.create {
		return fmt.Errorf("box: upload size required (missing from pending file id)")
	}
	return fmt.Errorf("box: upload size required for overwrite (use OpenWriteWithSize)")
}

func (w *boxWriter) uploadEmpty() error {
	op := "UploadFile"
	if w.create {
		op = "CreateFileUpload"
	}
	return w.bfs.withClassifiedRetry(w.ctx, op, func() error {
		client, err := w.bfs.session.apiClient(w.ctx)
		if err != nil {
			return err
		}
		if w.create {
			_, err = client.UploadNewFile(w.ctx, w.parentID, w.name, strings.NewReader(""), 0)
			return err
		}
		_, err = client.UploadFileVersion(w.ctx, w.fileID, strings.NewReader(""), 0)
		return err
	})
}

func (w *boxWriter) streamUpload(body io.Reader) error {
	op := "UploadFile"
	if w.create {
		op = "CreateFileUpload"
	}
	return w.bfs.withClassifiedRetry(w.ctx, op, func() error {
		client, err := w.bfs.session.apiClient(w.ctx)
		if err != nil {
			return err
		}
		if w.size <= SimpleUploadMaxBytes {
			if w.create {
				_, err = client.UploadNewFile(w.ctx, w.parentID, w.name, body, w.size)
				return err
			}
			_, err = client.UploadFileVersion(w.ctx, w.fileID, body, w.size)
			return err
		}
		return w.streamChunked(client, body)
	})
}

func (w *boxWriter) streamChunked(client *Client, body io.Reader) error {
	var sess uploadSession
	var err error
	if w.create {
		sess, err = client.CreateUploadSession(w.ctx, w.parentID, w.name, w.size)
	} else {
		sess, err = client.CreateUploadSessionForFile(w.ctx, w.fileID, w.size)
	}
	if err != nil {
		return err
	}
	partSize := sess.PartSize
	if partSize <= 0 {
		partSize = 8 * 1024 * 1024
	}
	uploadURL := sess.SessionEndpoints.UploadPart
	if uploadURL == "" {
		uploadURL = uploadHost + "/files/upload_sessions/" + url.PathEscape(sess.ID)
	}
	commitURL := sess.SessionEndpoints.Commit
	if commitURL == "" {
		commitURL = uploadHost + "/files/upload_sessions/" + url.PathEscape(sess.ID) + "/commit"
	}

	h := sha1.New()
	var parts []uploadPart
	buf := make([]byte, partSize)
	var offset int64
	for offset < w.size {
		need := int(partSize)
		if rem := w.size - offset; rem < int64(need) {
			need = int(rem)
		}
		n, readErr := io.ReadFull(body, buf[:need])
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		if n == 0 {
			break
		}
		chunk := buf[:n]
		if _, err := h.Write(chunk); err != nil {
			return err
		}
		part, err := client.UploadSessionPart(w.ctx, uploadURL, chunk, offset, w.size)
		if err != nil {
			return err
		}
		parts = append(parts, part)
		offset += int64(n)
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	contentSHA1 := base64.StdEncoding.EncodeToString(h.Sum(nil))
	_, err = client.CommitUploadSession(w.ctx, commitURL, parts, contentSHA1)
	return err
}

var _ io.WriteCloser = (*boxWriter)(nil)

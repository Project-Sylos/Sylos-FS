// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"codeberg.org/Sylos/Sylos-FS/pkg/fs/ctxstream"
)

const pendingFilePrefix = "pending:"
const pendingLocPrefix = "pending-loc:"
const pendingRootParent = "root"

func pendingParentRef(parentID string) string {
	if isDropboxRootRef(parentID) {
		return pendingRootParent
	}
	return parentID
}

func pendingFileID(parentID, name string) string {
	return fmt.Sprintf("%s%s:%s", pendingFilePrefix, pendingParentRef(parentID), name)
}

func pendingFileByLocation(locationPath string) string {
	return pendingLocPrefix + locationPath
}

func parsePendingFileID(fileID string) (parentID, name string, ok bool) {
	if !strings.HasPrefix(fileID, pendingFilePrefix) || strings.HasPrefix(fileID, pendingLocPrefix) {
		return "", "", false
	}
	parts := strings.SplitN(fileID, ":", 3)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func parsePendingLocationPath(fileID string) (locationPath string, ok bool) {
	if !strings.HasPrefix(fileID, pendingLocPrefix) {
		return "", false
	}
	loc := strings.TrimPrefix(fileID, pendingLocPrefix)
	if loc == "" || loc == "/" {
		return "", false
	}
	return loc, true
}

type dropboxWriter struct {
	dfs      *DropboxFS
	ctx      context.Context
	fileID   string
	parentID string
	fileName string
	locationPath string
	create   bool

	mu            sync.Mutex
	closed        bool
	uploadStarted bool
	out           io.WriteCloser
	uploadDone    chan error
	committedID   string
}

func newDropboxWriter(d *DropboxFS, ctx context.Context, fileID string) (*dropboxWriter, error) {
	w := &dropboxWriter{
		dfs:    d,
		ctx:    ctx,
		fileID: fileID,
	}
	if loc, ok := parsePendingLocationPath(fileID); ok {
		w.create = true
		w.locationPath = loc
	} else if parentID, name, ok := parsePendingFileID(fileID); ok {
		w.create = true
		w.parentID = parentID
		w.fileName = name
	}
	return w, nil
}

// CommittedServiceID returns the Dropbox file id after a successful create upload.
func (w *dropboxWriter) CommittedServiceID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.committedID
}

func (w *dropboxWriter) resolveUploadPath(client *Client) (string, error) {
	if !w.create {
		return dropboxPathRef(w.fileID), nil
	}
	if w.locationPath != "" {
		return w.dfs.resolveLocationPath(w.locationPath), nil
	}
	if err := w.dfs.errTeamSpaceRootWrite(); err != nil && isDropboxRootRef(w.parentID) {
		return "", err
	}
	parentID := w.dfs.normalizeParentForCreate(w.parentID)
	return client.resolveCreatePath(w.ctx, parentID, w.fileName, w.dfs.sharedRootPath())
}

func (w *dropboxWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return 0, fmt.Errorf("dropbox: writer closed")
	}
	if !w.uploadStarted {
		w.startUploadLocked()
	}
	out := w.out
	w.mu.Unlock()
	return out.Write(p)
}

func (w *dropboxWriter) Close() error {
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

func (w *dropboxWriter) startUploadLocked() {
	pr, pw := io.Pipe()
	w.out = ctxstream.NewWriteCloser(w.ctx, pw)
	w.uploadDone = make(chan error, 1)
	w.uploadStarted = true
	body := ctxstream.NewReader(w.ctx, pr)
	go func() {
		w.uploadDone <- w.streamUpload(body)
	}()
}

func (w *dropboxWriter) uploadEmpty() error {
	op := "UploadFile"
	if w.create {
		op = "CreateFileUpload"
	}
	return w.dfs.withClassifiedRetry(w.ctx, op, func() error {
		client, err := w.dfs.client(w.ctx)
		if err != nil {
			return err
		}
		uploadPath, err := w.resolveUploadPath(client)
		if err != nil {
			return err
		}
		meta, err := client.upload(w.ctx, uploadPath, strings.NewReader(""))
		if err != nil {
			return err
		}
		if meta.ID != "" {
			w.committedID = meta.ID
		}
		return nil
	})
}

func (w *dropboxWriter) streamUpload(body io.Reader) error {
	if testStreamUploadHook != nil {
		return testStreamUploadHook(w, body)
	}
	op := "UploadFile"
	if w.create {
		op = "CreateFileUpload"
	}
	return w.dfs.withClassifiedRetry(w.ctx, op, func() error {
		client, err := w.dfs.client(w.ctx)
		if err != nil {
			return err
		}
		uploadPath, err := w.resolveUploadPath(client)
		if err != nil {
			return err
		}
		meta, apiErr := client.uploadSession(w.ctx, uploadPath, body)
		if apiErr != nil {
			return apiErr
		}
		if meta.ID != "" {
			w.mu.Lock()
			w.committedID = meta.ID
			w.mu.Unlock()
		}
		if w.dfs.session.degradation != nil {
			w.dfs.session.degradation.ClearThrottleStreak()
		}
		return nil
	})
}

var testStreamUploadHook func(w *dropboxWriter, body io.Reader) error

func streamDownload(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	if body == nil {
		return io.NopCloser(strings.NewReader(""))
	}
	return ctxstream.NewReadCloser(ctx, body)
}

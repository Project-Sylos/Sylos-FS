// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/fs/ctxstream"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
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

type driveWriter struct {
	dfs      *DriveFS
	ctx      context.Context
	fileID   string
	parentID string
	name     string
	create   bool

	mu            sync.Mutex
	closed        bool
	uploadStarted bool
	out           io.WriteCloser
	uploadDone    chan error
}

func newDriveWriter(d *DriveFS, ctx context.Context, fileID string) (*driveWriter, error) {
	w := &driveWriter{
		dfs:    d,
		ctx:    ctx,
		fileID: fileID,
	}
	if parentID, name, ok := parsePendingFileID(fileID); ok {
		w.create = true
		w.parentID = parentID
		w.name = name
	}
	return w, nil
}

func (w *driveWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return 0, fmt.Errorf("google drive: writer closed")
	}
	if !w.uploadStarted {
		w.startUploadLocked()
	}
	out := w.out
	w.mu.Unlock()
	return out.Write(p)
}

func (w *driveWriter) Close() error {
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

func (w *driveWriter) startUploadLocked() {
	pr, pw := io.Pipe()
	w.out = ctxstream.NewWriteCloser(w.ctx, pw)
	w.uploadDone = make(chan error, 1)
	w.uploadStarted = true
	body := ctxstream.NewReader(w.ctx, pr)
	go func() {
		w.uploadDone <- w.streamUpload(body)
	}()
}

func (w *driveWriter) uploadEmpty() error {
	op := "UploadFile"
	if w.create {
		op = "CreateFileUpload"
	}
	return w.dfs.withClassifiedRetry(w.ctx, op, func() error {
		srv, err := w.dfs.session.driveService(w.ctx)
		if err != nil {
			return err
		}
		if w.create {
			meta := &drive.File{
				Name:    w.name,
				Parents: []string{w.parentID},
			}
			if w.dfs.ctx.RootType == cloud.RootTypeSharedDrive && w.dfs.ctx.DriveID != "" {
				meta.DriveId = w.dfs.ctx.DriveID
			}
			_, err = srv.Files.Create(meta).
				SupportsAllDrives(true).
				Fields("id,name,size,modifiedTime,parents").
				Do()
			return err
		}
		_, err = srv.Files.Update(w.fileID, &drive.File{}).SupportsAllDrives(true).Do()
		return err
	})
}

func (w *driveWriter) streamUpload(body io.Reader) error {
	if testStreamUploadHook != nil {
		return testStreamUploadHook(w, body)
	}
	srv, err := w.dfs.session.driveService(w.ctx)
	if err != nil {
		return err
	}
	mediaOpts := []googleapi.MediaOption{
		googleapi.ContentType("application/octet-stream"),
	}
	var apiErr error
	if w.create {
		meta := &drive.File{
			Name:    w.name,
			Parents: []string{w.parentID},
		}
		if w.dfs.ctx.RootType == cloud.RootTypeSharedDrive && w.dfs.ctx.DriveID != "" {
			meta.DriveId = w.dfs.ctx.DriveID
		}
		_, apiErr = srv.Files.Create(meta).
			SupportsAllDrives(true).
			Fields("id,name,size,modifiedTime,parents").
			Media(body, mediaOpts...).
			Do()
	} else {
		_, apiErr = srv.Files.Update(w.fileID, &drive.File{}).
			SupportsAllDrives(true).
			Media(body, mediaOpts...).
			Do()
	}
	if apiErr != nil {
		w.dfs.recordStreamUploadError(apiErr, w.create)
		return apiErr
	}
	if w.dfs.session.degradation != nil {
		w.dfs.session.degradation.ClearThrottleStreak()
	}
	return nil
}

// testStreamUploadHook is set by tests to observe upload bodies without calling the Drive API.
var testStreamUploadHook func(w *driveWriter, body io.Reader) error

func (d *DriveFS) recordStreamUploadError(err error, create bool) {
	op := "UploadFile"
	if create {
		op = "CreateFileUpload"
	}
	class := d.classifyError(err)
	switch class.Bucket {
	case types.FSErrorThrottle:
		d.recordDegradation(types.FSDegradationRateLimit, op, class.RetryAfter)
	case types.FSErrorRetryable:
		if class.RetryAfter > 0 {
			d.recordDegradation(types.FSDegradationHighLatency, op, class.RetryAfter)
		}
	}
}

// streamDownload wraps the Drive download body with context cancellation.
func streamDownload(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	if body == nil {
		return io.NopCloser(strings.NewReader(""))
	}
	return ctxstream.NewReadCloser(ctx, body)
}

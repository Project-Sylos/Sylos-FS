// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

const (
	exportSizeLimitReason = "exportSizeLimitExceeded"
	lroPollInitial        = 500 * time.Millisecond
	lroPollMax            = 10 * time.Second
)

type downloadFileResponse struct {
	DownloadURI string `json:"downloadUri"`
}

func isExportSizeLimited(err error) bool {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return false
	}
	if strings.Contains(strings.ToLower(gerr.Message), "too large to be exported") {
		return true
	}
	for _, e := range gerr.Errors {
		if e.Reason == exportSizeLimitReason {
			return true
		}
	}
	return false
}

func isExportConversionUnsupported(err error) bool {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return false
	}
	if gerr.Code == 400 {
		msg := strings.ToLower(gerr.Message)
		if strings.Contains(msg, "conversion is not supported") || strings.Contains(msg, "not supported") {
			return true
		}
	}
	return false
}

func (d *DriveFS) exportWorkspaceWithFallbacks(ctx context.Context, srv *drive.Service, fileID string, mimes []string) (io.ReadCloser, error) {
	var lastErr error
	for _, mime := range mimes {
		body, err := d.exportWorkspaceContent(ctx, srv, fileID, mime)
		if err == nil {
			return body, nil
		}
		if isExportConversionUnsupported(err) {
			lastErr = err
			continue
		}
		return nil, err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("google drive: no export format succeeded for %s", fileID)
}

func (d *DriveFS) exportWorkspaceContent(ctx context.Context, srv *drive.Service, fileID, mimeType string) (io.ReadCloser, error) {
	resp, err := srv.Files.Export(fileID, mimeType).Download()
	if err == nil {
		return resp.Body, nil
	}
	if !isExportSizeLimited(err) {
		return nil, fmt.Errorf("google drive: export %s as %s: %w", fileID, mimeType, err)
	}
	return d.exportWorkspaceViaLRO(ctx, srv, fileID, mimeType)
}

func (d *DriveFS) exportWorkspaceViaLRO(ctx context.Context, srv *drive.Service, fileID, mimeType string) (io.ReadCloser, error) {
	op, err := srv.Files.Download(fileID).MimeType(mimeType).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("google drive: start large export %s: %w", fileID, err)
	}
	op, err = pollDownloadOperation(ctx, srv, op)
	if err != nil {
		return nil, err
	}
	uri, err := downloadURIFromOperation(op)
	if err != nil {
		return nil, err
	}
	return d.downloadAuthenticated(ctx, uri)
}

func pollDownloadOperation(ctx context.Context, srv *drive.Service, op *drive.Operation) (*drive.Operation, error) {
	if op == nil {
		return nil, fmt.Errorf("google drive: nil download operation")
	}
	if op.Done {
		return op, operationFailure(op)
	}
	if op.Name == "" {
		return nil, fmt.Errorf("google drive: download operation missing name")
	}

	backoff := lroPollInitial
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < lroPollMax {
			backoff *= 2
			if backoff > lroPollMax {
				backoff = lroPollMax
			}
		}

		polled, err := srv.Operations.Get(op.Name).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("google drive: poll download operation: %w", err)
		}
		if polled.Done {
			return polled, operationFailure(polled)
		}
		op = polled
	}
}

func operationFailure(op *drive.Operation) error {
	if op == nil || op.Error == nil {
		return nil
	}
	if op.Error.Message != "" {
		return fmt.Errorf("google drive: download operation failed: %s", op.Error.Message)
	}
	return fmt.Errorf("google drive: download operation failed")
}

func downloadURIFromOperation(op *drive.Operation) (string, error) {
	if op == nil || len(op.Response) == 0 {
		return "", fmt.Errorf("google drive: download operation missing response")
	}
	var resp downloadFileResponse
	if err := json.Unmarshal(op.Response, &resp); err != nil {
		return "", fmt.Errorf("google drive: parse download operation response: %w", err)
	}
	if resp.DownloadURI == "" {
		return "", fmt.Errorf("google drive: download operation missing downloadUri")
	}
	return resp.DownloadURI, nil
}

func (d *DriveFS) downloadAuthenticated(ctx context.Context, uri string) (io.ReadCloser, error) {
	client, err := d.session.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("google drive: download exported content: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

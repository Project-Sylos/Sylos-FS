// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"encoding/json"
	"net/http"
	"testing"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

func TestIsExportSizeLimited(t *testing.T) {
	sizeErr := &googleapi.Error{
		Code:    http.StatusForbidden,
		Message: "This file is too large to be exported.",
		Errors:  []googleapi.ErrorItem{{Reason: exportSizeLimitReason}},
	}
	if !isExportSizeLimited(sizeErr) {
		t.Fatal("expected export size limit detection")
	}
	permErr := &googleapi.Error{
		Code:    http.StatusForbidden,
		Message: "insufficient permissions",
		Errors:  []googleapi.ErrorItem{{Reason: "insufficientFilePermissions"}},
	}
	if isExportSizeLimited(permErr) {
		t.Fatal("permissions error should not trigger LRO fallback")
	}
}

func TestDownloadURIFromOperation(t *testing.T) {
	payload, err := json.Marshal(downloadFileResponse{DownloadURI: "https://example.com/file.docx"})
	if err != nil {
		t.Fatal(err)
	}
	uri, err := downloadURIFromOperation(&drive.Operation{Done: true, Response: payload})
	if err != nil {
		t.Fatal(err)
	}
	if uri != "https://example.com/file.docx" {
		t.Fatalf("uri=%q", uri)
	}
}

func TestOperationFailure(t *testing.T) {
	if err := operationFailure(&drive.Operation{Done: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := operationFailure(&drive.Operation{
		Done:  true,
		Error: &drive.Status{Message: "boom"},
	}); err == nil {
		t.Fatal("expected operation error")
	}
}

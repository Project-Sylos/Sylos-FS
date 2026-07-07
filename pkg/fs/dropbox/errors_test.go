// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"encoding/json"
	"testing"
)

func TestExtractDropboxErrorTagNestedPath(t *testing.T) {
	raw := json.RawMessage(`{".tag":"path","path":{".tag":"not_found"}}`)
	if got := extractDropboxErrorTag(raw, ""); got != "path/not_found" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractDropboxErrorTagPrefersSummary(t *testing.T) {
	raw := json.RawMessage(`{".tag":"path","path":{".tag":"not_found"}}`)
	if got := extractDropboxErrorTag(raw, "path/not_found/extra detail"); got != "path/not_found" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractDropboxErrorTagUploadSession(t *testing.T) {
	raw := json.RawMessage(`{".tag":"incorrect_offset","correct_offset":1048576}`)
	if got := extractDropboxErrorTag(raw, ""); got != "incorrect_offset" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractDropboxErrorTagWriteOperations(t *testing.T) {
	raw := json.RawMessage(`{".tag":"too_many_write_operations"}`)
	if got := extractDropboxErrorTag(raw, ""); got != "too_many_write_operations" {
		t.Fatalf("got %q", got)
	}
}

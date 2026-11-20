// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"encoding/json"
	"testing"
)

func TestDecodeEntries_skipsSymlinks(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{".tag":"file","id":"id:1","name":"real.txt","path_display":"/real.txt","path_lower":"/real.txt","size":3}`),
		json.RawMessage(`{".tag":"file","id":"id:2","name":"link","path_display":"/link","path_lower":"/link","size":0,"symlink_info":{"target":"/real.txt"}}`),
		json.RawMessage(`{".tag":"folder","id":"id:3","name":"dir","path_display":"/dir","path_lower":"/dir"}`),
		json.RawMessage(`{".tag":"deleted","id":"id:4","name":"gone"}`),
	}
	got := decodeEntries(raw)
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (file + folder; symlink and deleted omitted)", len(got))
	}
	if got[0].Name != "real.txt" || got[0].Tag != "file" {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1].Name != "dir" || got[1].Tag != "folder" {
		t.Fatalf("second=%+v", got[1])
	}
}

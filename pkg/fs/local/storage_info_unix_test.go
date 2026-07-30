// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build unix

package local

import "testing"

func TestFilesystemUsageRoot(t *testing.T) {
	info, err := FilesystemUsage("/")
	if err != nil {
		t.Fatalf("FilesystemUsage: %v", err)
	}
	if !info.Available {
		t.Fatal("expected Available")
	}
	if info.TotalBytes <= 0 {
		t.Fatalf("TotalBytes=%d", info.TotalBytes)
	}
	if info.FreeBytes < 0 {
		t.Fatalf("FreeBytes=%d", info.FreeBytes)
	}
	if info.Source != "statfs" {
		t.Fatalf("Source=%q", info.Source)
	}
}

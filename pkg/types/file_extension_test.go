// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import "testing"

func TestFileExtension(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ILOVEYOU.txt.vbs", "vbs"},
		{"report.PDF", "pdf"},
		{"archive.tar.gz", "gz"},
		{"README", ""},
		{".gitignore", ""},
		{"/path/to/file.docx", "docx"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := FileExtension(tc.in); got != tc.want {
			t.Fatalf("FileExtension(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestStripOverlappingExtension(t *testing.T) {
	cases := []struct {
		name, ext, want string
	}{
		{"example.txt.txt", "txt", "example.txt"},
		{"ILOVEYOU.txt.vbs", "vbs", "ILOVEYOU.txt"},
		{"ILOVEYOU.txt.vbs", "txt", "ILOVEYOU.txt.vbs"}, // no suffix overlap with .txt
		{"report.PDF", "pdf", "report"},
		{"sylos.wiki", "wiki", "sylos"},
		{"readme", "txt", "readme"},
		{"file.txt", "", "file.txt"},
	}
	for _, tc := range cases {
		if got := StripOverlappingExtension(tc.name, tc.ext); got != tc.want {
			t.Fatalf("StripOverlappingExtension(%q,%q)=%q want %q", tc.name, tc.ext, got, tc.want)
		}
	}
}

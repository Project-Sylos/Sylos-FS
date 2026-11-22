// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import "testing"

func TestIsSkippedLinkStubName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Desktop.lnk", true},
		{"SITE.URL", true},
		{"notes.url", true},
		{"report.pdf", false},
		{"file.lnk.bak", false},
		{"lnk", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsSkippedLinkStubName(tc.in); got != tc.want {
			t.Fatalf("IsSkippedLinkStubName(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

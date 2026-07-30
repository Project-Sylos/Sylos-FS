// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import "testing"

func TestReportsOwnedStorageQuota(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rootType string
		want     bool
	}{
		{rootType: "", want: true},
		{rootType: RootTypeMyDrive, want: true},
		{rootType: RootTypeUserRoot, want: true},
		{rootType: RootTypeSharedDrive, want: true},
		{rootType: RootTypeSharePointDrive, want: true},
		{rootType: RootTypeTeamFolder, want: true},
		{rootType: RootTypeSharedWithMe, want: false},
		{rootType: RootTypeSharePointSite, want: false},
		{rootType: RootTypeTeamSpace, want: false},
		{rootType: RootTypeSharedFolder, want: true},
	}
	for _, tt := range cases {
		t.Run(tt.rootType+"_"+boolString(tt.want), func(t *testing.T) {
			t.Parallel()
			if got := ReportsOwnedStorageQuota(tt.rootType); got != tt.want {
				t.Fatalf("ReportsOwnedStorageQuota(%q)=%v want %v", tt.rootType, got, tt.want)
			}
		})
	}
}

func TestIsExternallyOwnedBrowseRoot(t *testing.T) {
	t.Parallel()
	if !IsExternallyOwnedBrowseRoot(RootTypeSharedWithMe) {
		t.Fatal("shared_with_me should be external")
	}
	if !IsExternallyOwnedBrowseRoot(RootTypeSharedFolder) {
		t.Fatal("shared_folder should be external")
	}
	if IsExternallyOwnedBrowseRoot(RootTypeMyDrive) {
		t.Fatal("my_drive should not be external")
	}
}

func boolString(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

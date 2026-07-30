// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import "strings"

// ReportsOwnedStorageQuota reports whether RootType refers to storage the signed-in
// account owns (or a real drive/library with its own quota).
//
// Virtual listings such as Shared with me aggregate other people's files and must
// not surface the account quota as if it applied to that space.
func ReportsOwnedStorageQuota(rootType string) bool {
	switch strings.TrimSpace(rootType) {
	case "":
		// Unspecified context defaults to account / primary drive quota.
		return true
	case RootTypeMyDrive, RootTypeUserRoot, RootTypeSharedDrive, RootTypeTeamFolder, RootTypeSharePointDrive:
		return true
	case RootTypeSharedWithMe, RootTypeSharePointSite, RootTypeTeamSpace:
		return false
	case RootTypeSharedFolder:
		// Dropbox shared-folder mounts still write against the signed-in account
		// (or team) allocation from users/get_space_usage. There is no per-folder
		// free-space API; report that account quota rather than Unavailable.
		return true
	default:
		return true
	}
}

// IsExternallyOwnedBrowseRoot reports whether the browse root is typically owned
// by another account or a shared space rather than the signed-in user's personal
// storage. Used for destination confirmation UX.
func IsExternallyOwnedBrowseRoot(rootType string) bool {
	switch strings.TrimSpace(rootType) {
	case RootTypeSharedWithMe, RootTypeSharedFolder, RootTypeSharedDrive, RootTypeTeamFolder:
		return true
	default:
		return false
	}
}

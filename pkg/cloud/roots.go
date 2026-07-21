// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import "strings"

// Root describes a selectable cloud browse entry (My Drive, shared drive, etc.).
type Root struct {
	ID                           string `json:"id"`
	DisplayName                  string `json:"displayName"`
	RootType                     string `json:"rootType"`
	DriveID                      string `json:"driveId,omitempty"`
	MigrationRootForbidden       bool   `json:"migrationRootForbidden,omitempty"`
	MigrationRootForbiddenReason string `json:"migrationRootForbiddenReason,omitempty"`
}

const (
	RootTypeMyDrive         = "my_drive"
	RootTypeSharedWithMe    = "shared_with_me"
	RootTypeSharedDrive     = "shared_drive"
	RootTypeUserRoot        = "user_root"
	RootTypeTeamSpace       = "team_space"
	RootTypeTeamFolder      = "team_folder"
	RootTypeSharedFolder    = "shared_folder"
	RootTypeSharePointSite  = "sharepoint_site"
	RootTypeSharePointDrive = "sharepoint_drive"
)

// IsVirtualRootListing reports whether identifier+rootType refers to opening a
// provider virtual root (vs a nested folder under that root).
// Nested browse should omit rootType and pass only driveId/namespace metadata.
func IsVirtualRootListing(identifier, rootType string) bool {
	id := strings.TrimSpace(identifier)
	rt := strings.TrimSpace(rootType)
	if rt == "" {
		return false
	}
	switch rt {
	case RootTypeUserRoot, RootTypeMyDrive:
		// Box All Files uses folder id "0".
		return id == "" || id == "root" || id == "0"
	case RootTypeTeamSpace:
		return id == "" || id == "root" || id == "teamSpace"
	case RootTypeSharedWithMe:
		return id == "sharedWithMe"
	case RootTypeTeamFolder, RootTypeSharedFolder, RootTypeSharedDrive, RootTypeSharePointDrive:
		// UI sends rootType only when opening these roots (identifier == root.id).
		return id != ""
	case RootTypeSharePointSite:
		return id != ""
	default:
		return false
	}
}

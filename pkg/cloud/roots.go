// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

// Root describes a selectable cloud browse entry (My Drive, shared drive, etc.).
type Root struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	RootType    string `json:"rootType"`
	DriveID     string `json:"driveId,omitempty"`
}

const (
	RootTypeMyDrive       = "my_drive"
	RootTypeSharedWithMe  = "shared_with_me"
	RootTypeSharedDrive   = "shared_drive"
	RootTypeUserRoot      = "user_root"
	RootTypeTeamSpace     = "team_space"
	RootTypeTeamFolder    = "team_folder"
	RootTypeSharedFolder  = "shared_folder"
)

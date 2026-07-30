// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import (
	"fmt"
	"strings"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// BrowseFolder builds a folder descriptor for pre-migration cloud browsing.
// rootType must match the /roots response when listing a virtual root (my_drive, shared_with_me, shared_drive, user_root, etc.).
// For nested folders, leave rootType empty and pass the folder's provider id as identifier.
// driveID carries namespace metadata for team_folder and shared_folder roots (from /roots driveId).
func BrowseFolder(identifier, rootType, driveID string) (types.Folder, error) {
	identifier = strings.TrimSpace(identifier)
	rootType = strings.TrimSpace(rootType)

	if rootType == "" {
		if identifier == "" {
			return types.Folder{}, fmt.Errorf("cloud browse: identifier is required")
		}
		return types.Folder{
			ServiceID: identifier,
			Type:      types.NodeTypeFolder,
		}, nil
	}

	switch rootType {
	case RootTypeSharedWithMe:
		return types.Folder{
			ServiceID:    "sharedWithMe",
			DisplayName:  "Shared with me",
			LocationPath: "/",
			Type:         RootTypeSharedWithMe,
		}, nil
	case RootTypeMyDrive:
		id := identifier
		if id == "" {
			id = "root"
		}
		return types.Folder{
			ServiceID:    id,
			LocationPath: "/",
			Type:         RootTypeMyDrive,
		}, nil
	case RootTypeSharedDrive:
		if identifier == "" {
			return types.Folder{}, fmt.Errorf("cloud browse: identifier (drive id) is required for shared_drive")
		}
		return types.Folder{
			ServiceID:    "root",
			ParentId:     identifier,
			DisplayName:  identifier,
			LocationPath: "/",
			Type:         RootTypeSharedDrive,
		}, nil
	case RootTypeUserRoot:
		ns := driveID
		if ns == "" {
			ns = identifier
		}
		if ns == "root" {
			ns = driveID
		}
		return types.Folder{
			ServiceID:    "root",
			ParentId:     ns,
			DisplayName:  "My Dropbox",
			LocationPath: "/",
			Type:         RootTypeUserRoot,
		}, nil
	case RootTypeTeamSpace:
		ns := driveID
		if ns == "" {
			ns = identifier
		}
		if ns == "" || ns == "root" || ns == "teamSpace" {
			return types.Folder{}, fmt.Errorf("cloud browse: driveId (namespace id) is required for team_space")
		}
		return types.Folder{
			ServiceID:    "teamSpace",
			ParentId:     ns,
			DisplayName:  "Team space",
			LocationPath: "/",
			Type:         RootTypeTeamSpace,
		}, nil
	case RootTypeTeamFolder:
		if identifier == "" {
			return types.Folder{}, fmt.Errorf("cloud browse: identifier (team folder id) is required for team_folder")
		}
		ns := driveID
		if ns == "" {
			ns = identifier
		}
		return types.Folder{
			ServiceID:    identifier,
			ParentId:     ns,
			DisplayName:  identifier,
			LocationPath: "/",
			Type:         RootTypeTeamFolder,
		}, nil
	case RootTypeSharedFolder:
		if identifier == "" {
			return types.Folder{}, fmt.Errorf("cloud browse: identifier (shared folder id) is required for shared_folder")
		}
		ns := driveID
		if ns == "" {
			ns = identifier
		}
		return types.Folder{
			ServiceID:    identifier,
			ParentId:     ns,
			DisplayName:  identifier,
			LocationPath: "/",
			Type:         RootTypeSharedFolder,
		}, nil
	case RootTypeSharePointSite:
		if identifier == "" {
			return types.Folder{}, fmt.Errorf("cloud browse: identifier (site id) is required for sharepoint_site")
		}
		return types.Folder{
			ServiceID:    identifier,
			DisplayName:  identifier,
			LocationPath: "/",
			Type:         RootTypeSharePointSite,
		}, nil
	case RootTypeSharePointDrive:
		if identifier == "" {
			return types.Folder{}, fmt.Errorf("cloud browse: identifier (drive id) is required for sharepoint_drive")
		}
		ns := driveID
		if ns == "" {
			ns = identifier
		}
		return types.Folder{
			ServiceID:    "root",
			ParentId:     ns,
			DisplayName:  identifier,
			LocationPath: "/",
			Type:         RootTypeSharePointDrive,
		}, nil
	default:
		return types.Folder{}, fmt.Errorf("cloud browse: unknown rootType %q", rootType)
	}
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import (
	"net/url"
	"strings"
)

// ItemPath builds a Graph path for a drive item.
// driveID empty uses /me/drive; folderID empty/"root" uses drive root.
func ItemPath(driveID, itemID string) string {
	itemID = strings.TrimSpace(itemID)
	driveID = strings.TrimSpace(driveID)
	if itemID == "" || itemID == "root" {
		if driveID == "" {
			return "/me/drive/root"
		}
		return "/drives/" + url.PathEscape(driveID) + "/root"
	}
	if driveID == "" {
		return "/me/drive/items/" + url.PathEscape(itemID)
	}
	return "/drives/" + url.PathEscape(driveID) + "/items/" + url.PathEscape(itemID)
}

// ChildrenPath is ItemPath + /children.
func ChildrenPath(driveID, itemID string) string {
	return ItemPath(driveID, itemID) + "/children"
}

// ContentPath is ItemPath + /content.
func ContentPath(driveID, itemID string) string {
	return ItemPath(driveID, itemID) + "/content"
}

// CreateUploadSessionPath for an existing item (overwrite) or parent+name via path.
func CreateUploadSessionPath(driveID, itemID string) string {
	return ItemPath(driveID, itemID) + "/createUploadSession"
}

// CreateUploadSessionByPath creates under a parent by filename path segment.
func CreateUploadSessionByPath(driveID, parentItemID, name string) string {
	parent := ItemPath(driveID, parentItemID)
	// Graph: POST /drives/{id}/items/{parent}:/{filename}:/createUploadSession
	if strings.HasSuffix(parent, "/root") {
		return strings.TrimSuffix(parent, "/root") + "/root:/" + pathEscapeName(name) + ":/createUploadSession"
	}
	return parent + ":/" + pathEscapeName(name) + ":/createUploadSession"
}

// PutContentByPath for small creates: PUT .../root:/name:/content or .../items/{id}:/name:/content
func PutContentByPath(driveID, parentItemID, name string) string {
	parent := ItemPath(driveID, parentItemID)
	if strings.HasSuffix(parent, "/root") {
		return strings.TrimSuffix(parent, "/root") + "/root:/" + pathEscapeName(name) + ":/content"
	}
	return parent + ":/" + pathEscapeName(name) + ":/content"
}

func pathEscapeName(name string) string {
	// Keep path separators encoded; Graph expects URL-encoded path segments.
	return strings.ReplaceAll(url.PathEscape(name), "+", "%20")
}

// DrivePath returns /me/drive or /drives/{id}.
func DrivePath(driveID string) string {
	driveID = strings.TrimSpace(driveID)
	if driveID == "" {
		return "/me/drive"
	}
	return "/drives/" + url.PathEscape(driveID)
}

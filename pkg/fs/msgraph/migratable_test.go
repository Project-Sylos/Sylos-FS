// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import "testing"

func TestMigratableDriveItem(t *testing.T) {
	folder := DriveItem{ID: "1", Name: "dir", Folder: &struct{}{}}
	file := DriveItem{ID: "2", Name: "a.txt", File: &struct{}{}}
	remoteFolder := DriveItem{
		ID:     "3",
		Name:   "shared-shortcut",
		Folder: &struct{}{},
		RemoteItem: &struct {
			ID string `json:"id"`
		}{ID: "remote"},
	}
	remoteFile := DriveItem{
		ID:   "4",
		Name: "link",
		File: &struct{}{},
		RemoteItem: &struct {
			ID string `json:"id"`
		}{ID: "remote"},
	}
	pkg := DriveItem{
		ID:   "5",
		Name: "Notebook",
		Package: &struct {
			Type string `json:"type"`
		}{Type: "oneNote"},
	}
	neither := DriveItem{ID: "6", Name: "odd"}

	if !MigratableDriveItem(folder) {
		t.Fatal("folder should be migratable")
	}
	if !MigratableDriveItem(file) {
		t.Fatal("file should be migratable")
	}
	if MigratableDriveItem(remoteFolder) {
		t.Fatal("remoteItem folder shortcut must be skipped")
	}
	if MigratableDriveItem(remoteFile) {
		t.Fatal("remoteItem file shortcut must be skipped")
	}
	if MigratableDriveItem(pkg) {
		t.Fatal("package items must be skipped")
	}
	if MigratableDriveItem(neither) {
		t.Fatal("items without file/folder facets must be skipped")
	}
}

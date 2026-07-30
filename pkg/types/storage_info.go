// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import "context"

// StorageInfo describes capacity / free space for a service root or account.
// When Available is false, FreeBytes/TotalBytes/UsedBytes are undefined.
type StorageInfo struct {
	Available  bool   `json:"available"`
	Unlimited  bool   `json:"unlimited,omitempty"`
	TotalBytes int64  `json:"totalBytes,omitempty"`
	UsedBytes  int64  `json:"usedBytes,omitempty"`
	FreeBytes  int64  `json:"freeBytes,omitempty"`
	Source     string `json:"source,omitempty"` // e.g. "statfs", "drive.about", "graph.quota"
}

// FSStorageInfo is implemented by adapters that can report storage capacity.
// Path may be ignored for account-level quotas (cloud); local uses it as the volume path.
type FSStorageInfo interface {
	GetStorageInfo(ctx context.Context, path string) (StorageInfo, error)
}

// StorageInfoFrom returns storage info when the adapter implements FSStorageInfo.
func StorageInfoFrom(adapter FSAdapter) (FSStorageInfo, bool) {
	if adapter == nil {
		return nil, false
	}
	s, ok := adapter.(FSStorageInfo)
	return s, ok && s != nil
}

// UnavailableStorage is the canonical response when a provider cannot report capacity.
func UnavailableStorage() StorageInfo {
	return StorageInfo{Available: false}
}

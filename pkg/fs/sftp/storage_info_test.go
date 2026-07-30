// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sftp

import (
	"context"
	"testing"
)

func TestGetStorageInfoUnavailable(t *testing.T) {
	f := &SftpFS{}
	info, err := f.GetStorageInfo(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatal("SFTP storage should be unavailable")
	}
}

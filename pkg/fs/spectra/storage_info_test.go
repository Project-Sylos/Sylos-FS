// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package spectra

import (
	"context"
	"testing"
)

func TestGetStorageInfoFake(t *testing.T) {
	s := &SpectraFS{}
	info, err := s.GetStorageInfo(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.FreeBytes != SpectraFakeFreeBytes {
		t.Fatalf("info=%+v", info)
	}
}

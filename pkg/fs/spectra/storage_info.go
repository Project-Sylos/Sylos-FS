// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package spectra

import (
	"context"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// SpectraFakeFreeBytes is the deterministic free space Spectra reports for UI/tests.
const SpectraFakeFreeBytes int64 = 10 << 40 // 10 TiB

// GetStorageInfo returns a deterministic fake capacity so Spectra exercises the storage UI path.
func (s *SpectraFS) GetStorageInfo(ctx context.Context, path string) (types.StorageInfo, error) {
	_ = ctx
	_ = path
	return types.StorageInfo{
		Available:  true,
		Unlimited:  false,
		TotalBytes: SpectraFakeFreeBytes,
		UsedBytes:  0,
		FreeBytes:  SpectraFakeFreeBytes,
		Source:     "spectra.fake",
	}, nil
}

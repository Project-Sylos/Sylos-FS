// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sftp

import (
	"testing"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestSftpFSExposesDegradationState(t *testing.T) {
	st := types.NewFSDegradationState()
	f := &SftpFS{session: &Session{degradation: st}}
	if f.GetDegradationState() != st {
		t.Fatal("GetDegradationState must expose session degradation for ME AIMD + UI")
	}
	f.RecordSignal(types.FSDegradationSignal{
		Kind:       types.FSDegradationRateLimit,
		RetryAfter: 2 * time.Second,
		Operation:  "ListChildren",
		At:         time.Now(),
	})
	snap := f.DegradationState()
	if snap.RateLimitedUntil.IsZero() || snap.RecentHits < 1 {
		t.Fatalf("expected rate-limit signal on reporter: %+v", snap)
	}
}

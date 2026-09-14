// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package optimeout

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestChunkScalesAndClamps(t *testing.T) {
	if got := Chunk(0); got != MinChunk {
		t.Fatalf("zero: %v", got)
	}
	if got := Chunk(8 << 20); got != MinChunk {
		t.Fatalf("8MiB: %v", got)
	}
	if got := Chunk(32 << 20); got != 32*time.Second {
		t.Fatalf("32MiB: %v", got)
	}
	if got := Chunk(1 << 40); got != MaxChunk {
		t.Fatalf("huge: %v", got)
	}
}

func TestDoChunkRetriesTransient(t *testing.T) {
	n := 0
	_, err := DoChunk(context.Background(), 64, func(ctx context.Context) (int, error) {
		n++
		if n < ChunkAttempts {
			return 0, context.DeadlineExceeded
		}
		return 7, nil
	})
	if err != nil || n != ChunkAttempts {
		t.Fatalf("err=%v n=%d", err, n)
	}
}

func TestDoChunkStopsOnParentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DoChunk(ctx, 64, func(context.Context) (int, error) {
		t.Fatal("fn should not run")
		return 0, nil
	})
	if err == nil {
		t.Fatal("expected parent cancel")
	}
}

func TestSoftHTTP(t *testing.T) {
	if !SoftHTTP(http.StatusBadGateway) || SoftHTTP(http.StatusBadRequest) {
		t.Fatal("soft http mismatch")
	}
}

func TestIsTransient(t *testing.T) {
	ctx := context.Background()
	if !IsTransient(ctx, context.DeadlineExceeded) || IsTransient(ctx, context.Canceled) {
		t.Fatal("deadline/cancel")
	}
	if !IsTransient(ctx, io.ErrUnexpectedEOF) {
		t.Fatal("unexpected EOF")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if IsTransient(canceled, context.DeadlineExceeded) {
		t.Fatal("parent canceled")
	}
}

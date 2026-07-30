// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import (
	"context"
	"io"
)
// FSTransferRestartPolicy declares how an adapter recovers mid-transfer after abandon.
// ME reads this the same way it reads CreateFolderBatchMax — no provider name switches.
type FSTransferRestartPolicy interface {
	// SupportsResumableTransfer reports whether the adapter can continue bytes from an
	// offset in a *new* provider session (SRC seek + continue write). Sessions are never
	// handed off between workers; resume always opens a fresh session then seeks.
	SupportsResumableTransfer() bool
	// RequiresDeleteBeforeRestart reports whether ME must DeleteNode(dst) before a full
	// restart. False means overwrite/clobber of a partial destination is safe.
	RequiresDeleteBeforeRestart() bool
}

// TransferRestartPolicyFrom returns the policy if adapter implements FSTransferRestartPolicy.
func TransferRestartPolicyFrom(adapter any) (FSTransferRestartPolicy, bool) {
	if adapter == nil {
		return nil, false
	}
	p, ok := adapter.(FSTransferRestartPolicy)
	return p, ok
}

// DefaultTransferRestartPolicy is used when an adapter does not declare a policy:
// not resumable, overwrite-safe (no delete before restart).
type DefaultTransferRestartPolicy struct{}

func (DefaultTransferRestartPolicy) SupportsResumableTransfer() bool  { return false }
func (DefaultTransferRestartPolicy) RequiresDeleteBeforeRestart() bool { return false }

// ResolveTransferRestartPolicy returns adapter policy or DefaultTransferRestartPolicy.
func ResolveTransferRestartPolicy(adapter any) FSTransferRestartPolicy {
	if p, ok := TransferRestartPolicyFrom(adapter); ok && p != nil {
		return p
	}
	return DefaultTransferRestartPolicy{}
}

// FSResumableWrite opens a destination write stream that continues at offset
// in a fresh provider session/handle (no live session handoff).
type FSResumableWrite interface {
	OpenWriteFromOffset(ctx context.Context, fileID string, offset int64) (io.WriteCloser, error)
}

// OpenWriteFromOffsetFrom returns FSResumableWrite if implemented.
func OpenWriteFromOffsetFrom(adapter any) (FSResumableWrite, bool) {
	if adapter == nil {
		return nil, false
	}
	w, ok := adapter.(FSResumableWrite)
	return w, ok
}

// FSOpenWriteWithSize opens a destination write stream with a declared content size.
// Providers that need size up-front for upload sessions (e.g. Box) implement this.
type FSOpenWriteWithSize interface {
	OpenWriteWithSize(ctx context.Context, fileID string, size int64) (io.WriteCloser, error)
}

// OpenWriteWithSizeFrom returns FSOpenWriteWithSize if implemented.
func OpenWriteWithSizeFrom(adapter any) (FSOpenWriteWithSize, bool) {
	if adapter == nil {
		return nil, false
	}
	w, ok := adapter.(FSOpenWriteWithSize)
	return w, ok
}

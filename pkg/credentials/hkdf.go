// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package credentials

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"
)

// HKDFInfo is the fixed HKDF-Expand info string for per-connection keys.
// Bump the version if derivation parameters change (incompatible with existing creds files).
const HKDFInfo = "sylos-fs/credentials/v1"

var (
	// ErrEmptyConnectionID is returned when connectionID is empty for DeriveConnectionKey.
	ErrEmptyConnectionID = errors.New("credentials: connection ID must be non-empty")
)

// DeriveConnectionKey returns a 32-byte AES-256 key derived from masterKey and connectionID
// using HKDF-SHA256 (RFC 5869). Salt is []byte(connectionID); IKM is masterKey.
//
// The engine stores one envelope master key and stable connection IDs; it does not need a
// separate key-to-file mapping. Paths to creds.conf may be public (e.g. named by connection ID).
// Wrong masterKey or connectionID yields decrypt failure when opening AES-GCM blobs.
func DeriveConnectionKey(masterKey []byte, connectionID string) ([]byte, error) {
	if len(masterKey) != KeySize {
		return nil, ErrInvalidKeyLength
	}
	if connectionID == "" {
		return nil, ErrEmptyConnectionID
	}
	out, err := hkdf.Key(sha256.New, masterKey, []byte(connectionID), HKDFInfo, KeySize)
	if err != nil {
		return nil, fmt.Errorf("credentials: hkdf: %w", err)
	}
	return out, nil
}

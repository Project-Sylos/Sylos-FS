// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package credentials provides AES-256-GCM encryption helpers for storing
// sensitive data (e.g. OAuth tokens) at rest, plus HKDF-based per-connection
// key derivation and a small helper for auth refresh / rate-limit retries.
//
// The engine should store one envelope master key (outside this package) and
// stable connection IDs. Use DeriveConnectionKey(masterKey, connectionID) before
// Encrypt/Decrypt so creds.conf paths need not be secret. Blobs encrypted with
// the raw master key (pre-HKDF) are incompatible with derived keys.
//
// This package does not persist keys or credential files.
package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const (
	// KeySize is the required length in bytes for AES-256 keys.
	KeySize = 32
	// NonceSize is the GCM nonce length used by Encrypt/Decrypt.
	NonceSize = 12
)

var (
	// ErrInvalidKeyLength is returned when masterKey is not KeySize bytes.
	ErrInvalidKeyLength = errors.New("credentials: master key must be 32 bytes for AES-256")
	// ErrCiphertextTooShort is returned when ciphertext is shorter than NonceSize.
	ErrCiphertextTooShort = errors.New("credentials: ciphertext too short")
)

// GenerateMasterKey returns a new random 32-byte key suitable for Encrypt/Decrypt.
func GenerateMasterKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("credentials: generate key: %w", err)
	}
	return key, nil
}

// Encrypt returns nonce || ciphertext || tag for the given plaintext using AES-256-GCM.
func Encrypt(plaintext, masterKey []byte) ([]byte, error) {
	if len(masterKey) != KeySize {
		return nil, ErrInvalidKeyLength
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("credentials: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("credentials: new gcm: %w", err)
	}
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("credentials: nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// Decrypt reverses Encrypt: expects nonce || ciphertext || tag.
func Decrypt(ciphertext, masterKey []byte) ([]byte, error) {
	if len(masterKey) != KeySize {
		return nil, ErrInvalidKeyLength
	}
	if len(ciphertext) < NonceSize {
		return nil, ErrCiphertextTooShort
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("credentials: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("credentials: new gcm: %w", err)
	}
	nonce := ciphertext[:NonceSize]
	sealed := ciphertext[NonceSize:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("credentials: decrypt: %w", err)
	}
	return plaintext, nil
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package credentials

import (
	"bytes"
	"testing"
)

func TestGenerateMasterKey(t *testing.T) {
	k1, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) != KeySize {
		t.Fatalf("key len = %d, want %d", len(k1), KeySize)
	}
	k2, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(k1, k2) {
		t.Fatal("expected two random keys to differ")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"access_token":"x","refresh_token":"y"}`)
	enc, err := Encrypt(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(enc) < NonceSize+16 { // at least nonce + tag
		t.Fatalf("ciphertext too small: %d", len(enc))
	}
	got, err := Decrypt(enc, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("plaintext mismatch: %q vs %q", got, plain)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Encrypt([]byte("secret"), key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decrypt(enc, wrong)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestEncryptInvalidKeyLength(t *testing.T) {
	_, err := Encrypt([]byte("x"), []byte("short"))
	if err != ErrInvalidKeyLength {
		t.Fatalf("Encrypt: got %v, want ErrInvalidKeyLength", err)
	}
}

func TestDecryptInvalidKeyLength(t *testing.T) {
	_, err := Decrypt(make([]byte, 32), []byte("short"))
	if err != ErrInvalidKeyLength {
		t.Fatalf("Decrypt: got %v, want ErrInvalidKeyLength", err)
	}
}

func TestDecryptCiphertextTooShort(t *testing.T) {
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decrypt([]byte{1, 2, 3}, key)
	if err != ErrCiphertextTooShort {
		t.Fatalf("Decrypt: got %v, want ErrCiphertextTooShort", err)
	}
}

func TestDeriveConnectionKeyDeterministic(t *testing.T) {
	mk, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	a, err := DeriveConnectionKey(mk, "conn-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := DeriveConnectionKey(mk, "conn-1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("DeriveConnectionKey should be deterministic for same inputs")
	}
	c, err := DeriveConnectionKey(mk, "conn-2")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, c) {
		t.Fatal("different connectionID should yield different derived keys")
	}
}

func TestDeriveConnectionKeyEmptyID(t *testing.T) {
	mk, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeriveConnectionKey(mk, "")
	if err != ErrEmptyConnectionID {
		t.Fatalf("got %v, want ErrEmptyConnectionID", err)
	}
}

func TestDeriveConnectionKeyWrongMasterFailsDecrypt(t *testing.T) {
	mk, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	derived, err := DeriveConnectionKey(mk, "conn-a")
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("secret payload")
	enc, err := Encrypt(plain, derived)
	if err != nil {
		t.Fatal(err)
	}
	wrongMK, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	wrongDerived, err := DeriveConnectionKey(wrongMK, "conn-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decrypt(enc, wrongDerived)
	if err == nil {
		t.Fatal("expected decrypt failure with wrong master key")
	}
}

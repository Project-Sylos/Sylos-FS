// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import (
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
)

func TestStoredCredentialsRoundTrip(t *testing.T) {
	masterKey, err := credentials.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	connectionID := "01TESTCONNECTION000000000000"
	stored := StoredCredentials{
		Provider:     ProviderGoogleDrive,
		RefreshToken: "refresh-secret",
		Scopes:       []string{"https://www.googleapis.com/auth/drive"},
		ClientID:     "client-id",
	}

	blob, err := EncryptStoredCredentials(stored, masterKey, connectionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blob) == 0 {
		t.Fatal("empty blob")
	}

	got, err := DecryptStoredCredentials(blob, masterKey, connectionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != stored.RefreshToken {
		t.Fatalf("refresh token mismatch: %q", got.RefreshToken)
	}
	if got.Provider != stored.Provider {
		t.Fatalf("provider mismatch: %q", got.Provider)
	}
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import "testing"

func TestStoredCredentialsFromOAuthDropbox(t *testing.T) {
	stored := StoredCredentialsFromOAuth(ProviderDropbox, "refresh", "cid", "secret", []string{"files.metadata.read"})
	if stored.TokenURI != "https://api.dropboxapi.com/oauth2/token" {
		t.Fatalf("TokenURI=%q", stored.TokenURI)
	}
	if stored.Provider != ProviderDropbox {
		t.Fatalf("Provider=%q", stored.Provider)
	}
}

func TestStoredCredentialsFromOAuthGoogleDrive(t *testing.T) {
	stored := StoredCredentialsFromOAuth(ProviderGoogleDrive, "refresh", "cid", "secret", nil)
	if stored.TokenURI != "https://oauth2.googleapis.com/token" {
		t.Fatalf("TokenURI=%q", stored.TokenURI)
	}
}

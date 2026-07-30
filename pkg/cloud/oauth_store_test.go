// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import "testing"

func TestStoredCredentialsFromOAuthDropbox(t *testing.T) {
	stored := StoredCredentialsFromOAuthTenant(ProviderDropbox, "refresh", "cid", "secret", []string{"files.metadata.read"}, "")
	if stored.TokenURI != "https://api.dropboxapi.com/oauth2/token" {
		t.Fatalf("TokenURI=%q", stored.TokenURI)
	}
	if stored.Provider != ProviderDropbox {
		t.Fatalf("Provider=%q", stored.Provider)
	}
}

func TestStoredCredentialsFromOAuthGoogleDrive(t *testing.T) {
	stored := StoredCredentialsFromOAuthTenant(ProviderGoogleDrive, "refresh", "cid", "secret", nil, "")
	if stored.TokenURI != "https://oauth2.googleapis.com/token" {
		t.Fatalf("TokenURI=%q", stored.TokenURI)
	}
}

func TestStoredCredentialsFromOAuthMicrosoft(t *testing.T) {
	for _, provider := range []string{ProviderOneDrive, ProviderSharePoint} {
		stored := StoredCredentialsFromOAuthTenant(provider, "refresh", "cid", "secret", nil, "")
		if stored.TokenURI != "https://login.microsoftonline.com/common/oauth2/v2.0/token" {
			t.Fatalf("%s TokenURI=%q", provider, stored.TokenURI)
		}
		tenanted := StoredCredentialsFromOAuthTenant(provider, "refresh", "cid", "secret", nil, "03087318-6294-4852-a7e9-8dfa58d92899")
		want := "https://login.microsoftonline.com/03087318-6294-4852-a7e9-8dfa58d92899/oauth2/v2.0/token"
		if tenanted.TokenURI != want {
			t.Fatalf("%s tenant TokenURI=%q want %q", provider, tenanted.TokenURI, want)
		}
	}
}

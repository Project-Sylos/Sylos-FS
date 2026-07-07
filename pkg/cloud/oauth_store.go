// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

// StoredCredentialsFromOAuth builds persisted credentials from a UI token POST.
func StoredCredentialsFromOAuth(providerID, refreshToken, clientID, clientSecret string, scopes []string) StoredCredentials {
	stored := StoredCredentials{
		Provider:     providerID,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       scopes,
	}
	switch providerID {
	case ProviderGoogleDrive:
		stored.TokenURI = "https://oauth2.googleapis.com/token"
	case ProviderDropbox:
		stored.TokenURI = "https://api.dropboxapi.com/oauth2/token"
	}
	return stored
}

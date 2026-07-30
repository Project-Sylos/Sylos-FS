// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import "codeberg.org/Sylos/Sylos-FS/pkg/fs/msgraph"

// StoredCredentialsFromOAuthTenant builds persisted credentials from a UI token POST.
// For OneDrive/SharePoint, empty microsoft tenant uses the multi-tenant "common" endpoint.
func StoredCredentialsFromOAuthTenant(providerID, refreshToken, clientID, clientSecret string, scopes []string, microsoftTenantID string) StoredCredentials {
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
	case ProviderOneDrive, ProviderSharePoint:
		stored.TokenURI = msgraph.TokenURLForTenant(microsoftTenantID)
	case ProviderBox:
		stored.TokenURI = "https://api.box.com/oauth2/token"
	}
	return stored
}

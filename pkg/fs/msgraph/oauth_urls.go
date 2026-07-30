// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import "strings"

// OAuthTenantOrCommon returns tenantID trimmed, or "common" when empty (multi-tenant apps).
func OAuthTenantOrCommon(tenantID string) string {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return "common"
	}
	return t
}

// TokenURLForTenant builds the Microsoft identity platform v2 token endpoint for a tenant.
func TokenURLForTenant(tenantID string) string {
	return "https://login.microsoftonline.com/" + OAuthTenantOrCommon(tenantID) + "/oauth2/v2.0/token"
}

// AuthURLForTenant builds the Microsoft identity platform v2 authorize endpoint for a tenant.
func AuthURLForTenant(tenantID string) string {
	return "https://login.microsoftonline.com/" + OAuthTenantOrCommon(tenantID) + "/oauth2/v2.0/authorize"
}

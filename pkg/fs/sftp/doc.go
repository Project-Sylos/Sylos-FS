// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package sftp provides a remote filesystem adapter over SFTP.
//
// Planned connection model (non-OAuth):
//   - UI POSTs host, port, username, and password or private key to the API
//   - API validates with a test ListChildren("/") call
//   - Credentials encrypted via cloud.StoredCredentials-style blob (no access tokens)
//   - ProviderID: cloud.ProviderSFTP
//
// Implementation follows local.LocalFS safety patterns for path traversal.
package sftp

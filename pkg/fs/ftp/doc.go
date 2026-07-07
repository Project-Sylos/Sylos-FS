// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package ftp provides a remote filesystem adapter over FTP/FTPS.
//
// Planned connection model mirrors pkg/fs/sftp: form-based credentials encrypted
// at rest, in-memory session only, ProviderID cloud.ProviderFTP.
package ftp

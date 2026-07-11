// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import (
	"fmt"
	"strings"
)

const defaultSFTPPort = 22

// StoredCredentialsFromSFTP builds persisted credentials from a form POST.
func StoredCredentialsFromSFTP(host, username, password, privateKey, keyPassphrase, hostKey string, port int) StoredCredentials {
	if port <= 0 {
		port = defaultSFTPPort
	}
	return StoredCredentials{
		Provider:      ProviderSFTP,
		Host:          strings.TrimSpace(host),
		Port:          port,
		Username:      strings.TrimSpace(username),
		Password:      password,
		PrivateKey:    strings.TrimSpace(privateKey),
		KeyPassphrase: keyPassphrase,
		HostKey:       strings.TrimSpace(hostKey),
	}
}

// ValidateSFTP reports whether stored credentials contain enough data to dial SFTP.
func (s StoredCredentials) ValidateSFTP() error {
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("sftp: host is required")
	}
	if strings.TrimSpace(s.Username) == "" {
		return fmt.Errorf("sftp: username is required")
	}
	if strings.TrimSpace(s.Password) == "" && strings.TrimSpace(s.PrivateKey) == "" {
		return fmt.Errorf("sftp: password or private_key is required")
	}
	if strings.TrimSpace(s.HostKey) == "" {
		return fmt.Errorf("sftp: host_key is required")
	}
	return nil
}

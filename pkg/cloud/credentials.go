// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import (
	"encoding/json"
	"fmt"

	"codeberg.org/Sylos/Sylos-FS/pkg/credentials"
)

// StoredCredentials is persisted as JSON inside the encrypted migration DB. Access tokens must never be stored.
type StoredCredentials struct {
	Provider     string   `json:"provider"`
	RefreshToken string   `json:"refresh_token"`
	Scopes       []string `json:"scopes,omitempty"`
	TokenURI     string   `json:"token_uri,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`

	// SFTP (non-OAuth form credentials)
	Host                   string `json:"host,omitempty"`
	Port                   int    `json:"port,omitempty"`
	Username               string `json:"username,omitempty"`
	Password               string `json:"password,omitempty"`
	PrivateKey             string `json:"private_key,omitempty"`
	KeyPassphrase          string `json:"key_passphrase,omitempty"`
	HostKey string `json:"host_key,omitempty"`
}

// EncryptStoredCredentials serializes and encrypts credentials with a derived connection key.
func EncryptStoredCredentials(stored StoredCredentials, masterKey []byte, connectionID string) ([]byte, error) {
	connKey, err := credentials.DeriveConnectionKey(masterKey, connectionID)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("cloud: marshal credentials: %w", err)
	}
	return credentials.Encrypt(raw, connKey)
}

// DecryptStoredCredentials decrypts and unmarshals stored credentials.
func DecryptStoredCredentials(blob []byte, masterKey []byte, connectionID string) (StoredCredentials, error) {
	connKey, err := credentials.DeriveConnectionKey(masterKey, connectionID)
	if err != nil {
		return StoredCredentials{}, err
	}
	raw, err := credentials.Decrypt(blob, connKey)
	if err != nil {
		return StoredCredentials{}, err
	}
	var stored StoredCredentials
	if err := json.Unmarshal(raw, &stored); err != nil {
		return StoredCredentials{}, fmt.Errorf("cloud: unmarshal credentials: %w", err)
	}
	return stored, nil
}

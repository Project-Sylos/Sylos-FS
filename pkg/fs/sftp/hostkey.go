// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sftp

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"

	"golang.org/x/crypto/ssh"
)

// HostKeyProbeResult is the SSH host key presented during the initial handshake.
type HostKeyProbeResult struct {
	HostKey     string // base64-encoded ssh.PublicKey.Marshal()
	Fingerprint string // OpenSSH-style SHA256 fingerprint
}

// FetchServerHostKey dials host:port and captures the server's SSH host key without
// authenticating. Used for trust-on-first-use pinning in the UI.
func FetchServerHostKey(host string, port int) (HostKeyProbeResult, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return HostKeyProbeResult{}, fmt.Errorf("sftp host key probe: host is required")
	}
	if port <= 0 {
		port = 22
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	var captured ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User: "sylos-probe",
		// Intentionally empty: we only need the host key from the handshake.
		Auth: []ssh.AuthMethod{},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			captured = key
			return nil
		},
		Timeout: dialTimeout,
	}

	conn, err := ssh.Dial("tcp", addr, cfg)
	if conn != nil {
		_ = conn.Close()
	}
	if captured == nil {
		if err != nil {
			return HostKeyProbeResult{}, fmt.Errorf("sftp host key probe: %w", err)
		}
		return HostKeyProbeResult{}, fmt.Errorf("sftp host key probe: no host key received")
	}

	return HostKeyProbeResult{
		HostKey:     base64.StdEncoding.EncodeToString(captured.Marshal()),
		Fingerprint: ssh.FingerprintSHA256(captured),
	}, nil
}

// FingerprintHostKey returns the OpenSSH SHA256 fingerprint for a base64-encoded host key.
func FingerprintHostKey(hostKeyB64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(hostKeyB64))
	if err != nil {
		return "", fmt.Errorf("sftp host key fingerprint: %w", err)
	}
	pubKey, err := ssh.ParsePublicKey(raw)
	if err != nil {
		return "", fmt.Errorf("sftp host key fingerprint: %w", err)
	}
	return ssh.FingerprintSHA256(pubKey), nil
}

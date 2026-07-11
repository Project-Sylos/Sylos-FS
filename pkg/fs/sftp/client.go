// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sftp

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const dialTimeout = 15 * time.Second

// Client wraps a shared SSH + SFTP connection.
type Client struct {
	ssh  *ssh.Client
	sftp *sftp.Client
}

func Dial(stored cloud.StoredCredentials) (*Client, error) {
	if err := stored.ValidateSFTP(); err != nil {
		return nil, err
	}
	port := stored.Port
	if port <= 0 {
		port = 22
	}
	addr := net.JoinHostPort(strings.TrimSpace(stored.Host), fmt.Sprintf("%d", port))

	hostKeyCallback, err := hostKeyCallback(stored)
	if err != nil {
		return nil, err
	}

	authMethods, err := authMethods(stored)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            strings.TrimSpace(stored.Username),
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         dialTimeout,
	}

	conn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("sftp ssh dial: %w", err)
	}

	sftpClient, err := sftp.NewClient(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sftp client: %w", err)
	}

	return &Client{ssh: conn, sftp: sftpClient}, nil
}

func authMethods(stored cloud.StoredCredentials) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if pw := stored.Password; strings.TrimSpace(pw) != "" {
		methods = append(methods, ssh.Password(pw))
	}
	if keyPEM := strings.TrimSpace(stored.PrivateKey); keyPEM != "" {
		var signer ssh.Signer
		var err error
		if stored.KeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(keyPEM), []byte(stored.KeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(keyPEM))
		}
		if err != nil {
			return nil, fmt.Errorf("sftp private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("sftp: no auth methods configured")
	}
	return methods, nil
}

func hostKeyCallback(stored cloud.StoredCredentials) (ssh.HostKeyCallback, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(stored.HostKey))
	if err != nil {
		return nil, fmt.Errorf("sftp host_key must be base64-encoded host key bytes: %w", err)
	}
	pubKey, err := ssh.ParsePublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("sftp host_key: %w", err)
	}
	return ssh.FixedHostKey(pubKey), nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.sftp != nil {
		_ = c.sftp.Close()
	}
	if c.ssh != nil {
		return c.ssh.Close()
	}
	return nil
}

func (c *Client) SFTP() *sftp.Client {
	return c.sftp
}

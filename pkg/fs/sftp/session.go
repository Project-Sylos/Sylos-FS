// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sftp

import (
	"context"
	"fmt"
	"sync"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func init() {
	cloud.RegisterFactory(factory{})
}

type factory struct{}

func (factory) ProviderID() string { return cloud.ProviderSFTP }

func (factory) NewSession(connectionID string, stored cloud.StoredCredentials, _ *cloud.TokenStore, degradation *types.FSDegradationState) (cloud.Session, error) {
	if err := stored.ValidateSFTP(); err != nil {
		return nil, err
	}
	client, err := Dial(stored)
	if err != nil {
		return nil, err
	}
	if degradation == nil {
		degradation = types.NewFSDegradationState()
	}
	return &Session{
		connectionID: connectionID,
		stored:       stored,
		client:       client,
		degradation:  degradation,
	}, nil
}

func (f factory) ListRoots(ctx context.Context, session cloud.Session) ([]cloud.Root, error) {
	_ = ctx
	s, ok := session.(*Session)
	if !ok {
		return nil, fmt.Errorf("sftp: invalid session type")
	}
	if !s.HasValidCredentials() {
		return nil, fmt.Errorf("sftp: session not connected")
	}
	return []cloud.Root{{
		ID:          "/",
		DisplayName: s.stored.Host,
		RootType:    cloud.RootTypeUserRoot,
	}}, nil
}

// Session holds one SSH/SFTP connection for a migration side.
type Session struct {
	mu           sync.RWMutex
	connectionID string
	stored       cloud.StoredCredentials
	client       *Client
	degradation  *types.FSDegradationState
	closed       bool
}

func (s *Session) ConnectionID() string { return s.connectionID }
func (s *Session) ProviderID() string   { return cloud.ProviderSFTP }

func (s *Session) DegradationState() *types.FSDegradationState { return s.degradation }

func (s *Session) HasValidCredentials() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.closed && s.client != nil
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.client != nil {
		err := s.client.Close()
		s.client = nil
		return err
	}
	return nil
}

func (s *Session) CreateAdapter(rootFolder types.Folder) (types.FSAdapter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || s.client == nil {
		return nil, fmt.Errorf("sftp session closed")
	}
	rootPath := normalizeRemotePath(rootFolder.ServiceID)
	return &SftpFS{
		session: s,
		root:    rootFolder,
		rootAbs: rootPath,
		client:  s.client,
	}, nil
}

func (s *Session) RefreshAccessToken(_ context.Context) error {
	return nil
}

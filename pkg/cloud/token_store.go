// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import (
	"sync"
	"time"
)

// TokenStore holds in-memory access tokens keyed by connection ID.
type TokenStore struct {
	mu     sync.RWMutex
	tokens map[string]accessTokenEntry
}

type accessTokenEntry struct {
	token  string
	expiry time.Time
}

// NewTokenStore creates an empty token store.
func NewTokenStore() *TokenStore {
	return &TokenStore{tokens: make(map[string]accessTokenEntry)}
}

// DefaultTokenStore is the process-wide in-memory access token cache.
var DefaultTokenStore = NewTokenStore()

// SetAccessToken stores an access token for a connection.
func (s *TokenStore) SetAccessToken(connectionID, token string, expiry time.Time) {
	if s == nil || connectionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[connectionID] = accessTokenEntry{token: token, expiry: expiry}
}

// GetAccessToken returns the cached access token if present and not expired.
func (s *TokenStore) GetAccessToken(connectionID string) (string, bool) {
	if s == nil || connectionID == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tokens[connectionID]
	if !ok || entry.token == "" {
		return "", false
	}
	if !entry.expiry.IsZero() && time.Now().After(entry.expiry.Add(-30*time.Second)) {
		return "", false
	}
	return entry.token, true
}

// ClearAccessToken removes the cached access token for a connection.
func (s *TokenStore) ClearAccessToken(connectionID string) {
	if s == nil || connectionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, connectionID)
}

// ClearConnection removes all in-memory state for a connection.
func (s *TokenStore) ClearConnection(connectionID string) {
	s.ClearAccessToken(connectionID)
}

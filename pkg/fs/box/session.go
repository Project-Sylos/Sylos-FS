// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package box

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"golang.org/x/oauth2"
)

const (
	boxAuthURL  = "https://account.box.com/api/oauth2/authorize"
	boxTokenURL = "https://api.box.com/oauth2/token"
	rootFolderID = "0"
)

func init() {
	cloud.RegisterFactory(factory{})
}

type factory struct{}

func (factory) ProviderID() string { return cloud.ProviderBox }
func (factory) ForbiddenMigrationRootIDs() []string {
	return nil
}

func (factory) NewSession(connectionID string, stored cloud.StoredCredentials, tokens *cloud.TokenStore, degradation *types.FSDegradationState) (cloud.Session, error) {
	if stored.RefreshToken == "" {
		return nil, fmt.Errorf("box: refresh token required")
	}
	if tokens == nil {
		tokens = cloud.DefaultTokenStore
	}
	if degradation == nil {
		degradation = types.NewFSDegradationState()
	}
	return &Session{
		connectionID: connectionID,
		stored:       stored,
		tokens:       tokens,
		degradation:  degradation,
	}, nil
}

func (f factory) ListRoots(ctx context.Context, session cloud.Session) ([]cloud.Root, error) {
	s, ok := session.(*Session)
	if !ok {
		return nil, fmt.Errorf("box: invalid session type")
	}
	// Verify credentials with a lightweight call.
	client, err := s.apiClient(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := client.GetCurrentUser(ctx); err != nil {
		return nil, err
	}
	return []cloud.Root{
		{
			ID:          rootFolderID,
			DisplayName: "All Files",
			RootType:    cloud.RootTypeMyDrive,
		},
	}, nil
}

// Session holds Box OAuth state for one connection.
type Session struct {
	mu           sync.RWMutex
	refreshMu    sync.Mutex
	connectionID string
	stored       cloud.StoredCredentials
	tokens       *cloud.TokenStore
	degradation  *types.FSDegradationState
	persistCreds func(cloud.StoredCredentials) error
	closed       bool
}

func (s *Session) ConnectionID() string                        { return s.connectionID }
func (s *Session) ProviderID() string                          { return cloud.ProviderBox }
func (s *Session) DegradationState() *types.FSDegradationState { return s.degradation }

func (s *Session) SetCredentialsPersist(fn func(cloud.StoredCredentials) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistCreds = fn
}

func (s *Session) HasValidCredentials() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.closed && s.stored.RefreshToken != ""
}

func (s *Session) ExportStoredCredentials() (cloud.StoredCredentials, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return cloud.StoredCredentials{}, fmt.Errorf("box session closed")
	}
	if s.stored.RefreshToken == "" {
		return cloud.StoredCredentials{}, fmt.Errorf("box: no refresh token to export")
	}
	return s.stored, nil
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.tokens.ClearAccessToken(s.connectionID)
	return nil
}

func (s *Session) CreateAdapter(rootFolder types.Folder) (types.FSAdapter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, fmt.Errorf("box session closed")
	}
	return &BoxFS{
		session:  s,
		root:     rootFolder,
		folderID: resolveRootFolderID(rootFolder),
	}, nil
}

func resolveRootFolderID(folder types.Folder) string {
	id := folder.ServiceID
	if id == "" || id == "root" {
		return rootFolderID
	}
	return id
}

func (s *Session) RefreshAccessToken(ctx context.Context) error {
	token, expiry, err := s.refreshToken(ctx)
	if err != nil {
		return err
	}
	s.tokens.SetAccessToken(s.connectionID, token, expiry)
	return nil
}

func (s *Session) ClearAccessToken() {
	s.tokens.ClearAccessToken(s.connectionID)
}

func (s *Session) Degradation() *types.FSDegradationState { return s.degradation }

func (s *Session) refreshToken(ctx context.Context) (string, time.Time, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	s.mu.RLock()
	refresh := s.stored.RefreshToken
	cfg := &oauth2.Config{
		ClientID:     s.stored.ClientID,
		ClientSecret: s.stored.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  boxAuthURL,
			TokenURL: boxTokenURL,
		},
		Scopes: s.stored.Scopes,
	}
	s.mu.RUnlock()

	src := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refresh})
	tok, err := src.Token()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("box refresh: %w", err)
	}
	if tok.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("box refresh: empty access token")
	}

	// Box refresh tokens are single-use; persist the rotated token immediately.
	if tok.RefreshToken != "" && tok.RefreshToken != refresh {
		s.mu.Lock()
		s.stored.RefreshToken = tok.RefreshToken
		stored := s.stored
		persist := s.persistCreds
		s.mu.Unlock()
		if persist != nil {
			if err := persist(stored); err != nil {
				return "", time.Time{}, fmt.Errorf("box refresh: persist rotated refresh token: %w", err)
			}
		}
	}
	return tok.AccessToken, tok.Expiry, nil
}

func (s *Session) httpClient(ctx context.Context) (*http.Client, error) {
	if token, ok := s.tokens.GetAccessToken(s.connectionID); ok {
		src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"})
		return oauth2.NewClient(ctx, src), nil
	}
	if err := s.RefreshAccessToken(ctx); err != nil {
		return nil, err
	}
	token, ok := s.tokens.GetAccessToken(s.connectionID)
	if !ok {
		return nil, fmt.Errorf("box: no access token after refresh")
	}
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"})
	return oauth2.NewClient(ctx, src), nil
}

func (s *Session) apiClient(ctx context.Context) (*Client, error) {
	hc, err := s.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	return newClient(hc), nil
}

func (s *Session) ResolveAccountIdentity(ctx context.Context) (cloud.AccountIdentity, error) {
	client, err := s.apiClient(ctx)
	if err != nil {
		return cloud.AccountIdentity{}, err
	}
	user, err := client.GetCurrentUser(ctx)
	if err != nil {
		return cloud.AccountIdentity{}, err
	}
	return cloud.AccountIdentity{Email: user.Login, DisplayName: user.Name}, nil
}

func (s *Session) PrimeAccessToken(accessToken string, expiresInSec int64) {
	expiry := time.Time{}
	if expiresInSec > 0 {
		expiry = time.Now().Add(time.Duration(expiresInSec) * time.Second)
	}
	s.tokens.SetAccessToken(s.connectionID, accessToken, expiry)
}

var (
	_ cloud.CredentialsExporter  = (*Session)(nil)
	_ cloud.CredentialsPersister = (*Session)(nil)
	_ cloud.AccountResolver      = (*Session)(nil)
)

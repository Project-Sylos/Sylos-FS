// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package onedrive

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/fs/msgraph"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"golang.org/x/oauth2"
)

func init() {
	cloud.RegisterFactory(factory{})
}

type factory struct{}

func (factory) ProviderID() string { return cloud.ProviderOneDrive }
func (factory) ForbiddenMigrationRootIDs() []string {
	return []string{"sharedWithMe"}
}

func (factory) NewSession(connectionID string, stored cloud.StoredCredentials, tokens *cloud.TokenStore, degradation *types.FSDegradationState) (cloud.Session, error) {
	if stored.RefreshToken == "" {
		return nil, fmt.Errorf("onedrive: refresh token required")
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
		return nil, fmt.Errorf("onedrive: invalid session type")
	}
	client, err := s.graphClient(ctx)
	if err != nil {
		return nil, err
	}
	drive, err := client.GetDrive(ctx, "/me/drive")
	if err != nil {
		return nil, err
	}
	roots := []cloud.Root{
		{
			ID:          "root",
			DisplayName: "My files",
			RootType:    cloud.RootTypeMyDrive,
			DriveID:     drive.ID,
		},
		{
			ID:          "sharedWithMe",
			DisplayName: "Shared",
			RootType:    cloud.RootTypeSharedWithMe,
		},
	}
	return roots, nil
}

// Session holds OneDrive OAuth state for one connection.
type Session struct {
	mu           sync.RWMutex
	connectionID string
	stored       cloud.StoredCredentials
	tokens       *cloud.TokenStore
	degradation  *types.FSDegradationState
	closed       bool
}

func (s *Session) ConnectionID() string                     { return s.connectionID }
func (s *Session) ProviderID() string                       { return cloud.ProviderOneDrive }
func (s *Session) DegradationState() *types.FSDegradationState { return s.degradation }

func (s *Session) HasValidCredentials() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.closed && s.stored.RefreshToken != ""
}

func (s *Session) ExportStoredCredentials() (cloud.StoredCredentials, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return cloud.StoredCredentials{}, fmt.Errorf("onedrive session closed")
	}
	if s.stored.RefreshToken == "" {
		return cloud.StoredCredentials{}, fmt.Errorf("onedrive: no refresh token to export")
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
		return nil, fmt.Errorf("onedrive session closed")
	}
	return &OneDriveFS{
		ops: msgraph.AdapterOps{
			Auth: s,
			Root: rootFolder,
			Ctx:  parseDriveContext(rootFolder),
		},
	}, nil
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

func (s *Session) GraphClient(ctx context.Context) (*msgraph.Client, error) {
	return s.graphClient(ctx)
}

func (s *Session) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.stored.ClientID,
		ClientSecret: s.stored.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  msgraph.AuthURL,
			TokenURL: msgraph.TokenURL,
		},
		Scopes: s.stored.Scopes,
	}
}

func (s *Session) refreshToken(ctx context.Context) (string, time.Time, error) {
	cfg := s.oauthConfig()
	src := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: s.stored.RefreshToken})
	tok, err := src.Token()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("onedrive refresh: %w", err)
	}
	if tok.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("onedrive refresh: empty access token")
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
		return nil, fmt.Errorf("onedrive: no access token after refresh")
	}
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"})
	return oauth2.NewClient(ctx, src), nil
}

func (s *Session) graphClient(ctx context.Context) (*msgraph.Client, error) {
	hc, err := s.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	return msgraph.NewClient(hc), nil
}

func (s *Session) ResolveAccountIdentity(ctx context.Context) (cloud.AccountIdentity, error) {
	client, err := s.graphClient(ctx)
	if err != nil {
		return cloud.AccountIdentity{}, err
	}
	email, name, err := client.Me(ctx)
	if err != nil {
		return cloud.AccountIdentity{}, err
	}
	return cloud.AccountIdentity{Email: email, DisplayName: name}, nil
}

func (s *Session) PrimeAccessToken(accessToken string, expiresInSec int64) {
	expiry := time.Time{}
	if expiresInSec > 0 {
		expiry = time.Now().Add(time.Duration(expiresInSec) * time.Second)
	}
	s.tokens.SetAccessToken(s.connectionID, accessToken, expiry)
}

func parseDriveContext(folder types.Folder) msgraph.DriveContext {
	ctx := msgraph.DriveContext{
		FolderID: folder.ServiceID,
		DriveID:  folder.ParentId,
	}
	if ctx.FolderID == "" {
		ctx.FolderID = "root"
	}
	switch folder.Type {
	case cloud.RootTypeSharedWithMe:
		ctx.RootType = cloud.RootTypeSharedWithMe
		ctx.FolderID = folder.ServiceID
	case cloud.RootTypeMyDrive, "":
		ctx.RootType = cloud.RootTypeMyDrive
		if ctx.FolderID == "root" || ctx.FolderID == "" {
			ctx.FolderID = "root"
		}
	default:
		if folder.Type != "" && folder.Type != types.NodeTypeFolder {
			ctx.RootType = folder.Type
		} else {
			ctx.RootType = cloud.RootTypeMyDrive
		}
	}
	return ctx
}

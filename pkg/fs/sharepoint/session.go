// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sharepoint

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

func (factory) ProviderID() string { return cloud.ProviderSharePoint }
func (factory) ForbiddenMigrationRootIDs() []string {
	return nil
}

func (factory) NewSession(connectionID string, stored cloud.StoredCredentials, tokens *cloud.TokenStore, degradation *types.FSDegradationState) (cloud.Session, error) {
	if stored.RefreshToken == "" {
		return nil, fmt.Errorf("sharepoint: refresh token required")
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
		return nil, fmt.Errorf("sharepoint: invalid session type")
	}
	client, err := s.GraphClient(ctx)
	if err != nil {
		return nil, err
	}
	sites, err := client.ListSitesSearch(ctx, "*")
	if err != nil {
		return nil, err
	}
	roots := make([]cloud.Root, 0, len(sites))
	for _, site := range sites {
		if site.IsPersonalSite {
			// Personal OneDrive sites belong under the OneDrive provider.
			continue
		}
		name := site.DisplayName
		if name == "" {
			name = site.Name
		}
		if name == "" {
			name = site.WebURL
		}
		roots = append(roots, cloud.Root{
			ID:                           site.ID,
			DisplayName:                  name,
			RootType:                     cloud.RootTypeSharePointSite,
			MigrationRootForbidden:       true,
			MigrationRootForbiddenReason: "Select a document library or folder inside this site — the site itself cannot be a migration root.",
		})
	}
	return roots, nil
}

// Session holds SharePoint OAuth state for one connection.
type Session struct {
	mu           sync.RWMutex
	connectionID string
	stored       cloud.StoredCredentials
	tokens       *cloud.TokenStore
	degradation  *types.FSDegradationState
	closed       bool
}

func (s *Session) ConnectionID() string                        { return s.connectionID }
func (s *Session) ProviderID() string                          { return cloud.ProviderSharePoint }
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
		return cloud.StoredCredentials{}, fmt.Errorf("sharepoint session closed")
	}
	if s.stored.RefreshToken == "" {
		return cloud.StoredCredentials{}, fmt.Errorf("sharepoint: no refresh token to export")
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
		return nil, fmt.Errorf("sharepoint session closed")
	}
	return &SharePointFS{
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

func (s *Session) oauthConfig() *oauth2.Config {
	tokenURL := msgraph.TokenURL
	if s.stored.TokenURI != "" {
		tokenURL = s.stored.TokenURI
	}
	return &oauth2.Config{
		ClientID:     s.stored.ClientID,
		ClientSecret: s.stored.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  msgraph.AuthURL,
			TokenURL: tokenURL,
		},
		Scopes: s.stored.Scopes,
	}
}

func (s *Session) refreshToken(ctx context.Context) (string, time.Time, error) {
	cfg := s.oauthConfig()
	src := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: s.stored.RefreshToken})
	tok, err := src.Token()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sharepoint refresh: %w", err)
	}
	if tok.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("sharepoint refresh: empty access token")
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
		return nil, fmt.Errorf("sharepoint: no access token after refresh")
	}
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"})
	return oauth2.NewClient(ctx, src), nil
}

func (s *Session) GraphClient(ctx context.Context) (*msgraph.Client, error) {
	hc, err := s.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	return msgraph.NewClient(hc), nil
}

func (s *Session) ResolveAccountIdentity(ctx context.Context) (cloud.AccountIdentity, error) {
	client, err := s.GraphClient(ctx)
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
		RootType: folder.Type,
	}
	switch folder.Type {
	case cloud.RootTypeSharePointSite:
		ctx.SiteID = folder.ServiceID
		ctx.FolderID = folder.ServiceID
		ctx.DriveID = ""
	case cloud.RootTypeSharePointDrive:
		ctx.DriveID = folder.ParentId
		if ctx.DriveID == "" {
			ctx.DriveID = folder.ServiceID
		}
		ctx.FolderID = "root"
	default:
		if ctx.FolderID == "" {
			ctx.FolderID = "root"
		}
		if ctx.RootType == "" || ctx.RootType == types.NodeTypeFolder {
			ctx.RootType = cloud.RootTypeSharePointDrive
		}
	}
	return ctx
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

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

const dropboxTokenURL = "https://api.dropboxapi.com/oauth2/token"

func init() {
	cloud.RegisterFactory(factory{})
}

type factory struct{}

func (factory) ProviderID() string { return cloud.ProviderDropbox }

func (factory) NewSession(connectionID string, stored cloud.StoredCredentials, tokens *cloud.TokenStore, degradation *types.FSDegradationState) (cloud.Session, error) {
	if stored.RefreshToken == "" {
		return nil, fmt.Errorf("dropbox: refresh token required")
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
	if ok {
		return s.listRoots(ctx)
	}
	return nil, fmt.Errorf("dropbox: invalid session type")
}

// Session holds Dropbox OAuth state for one connection.
type Session struct {
	mu           sync.RWMutex
	connectionID string
	stored       cloud.StoredCredentials
	tokens       *cloud.TokenStore
	degradation  *types.FSDegradationState
	closed       bool
}

func (s *Session) ConnectionID() string { return s.connectionID }
func (s *Session) ProviderID() string   { return cloud.ProviderDropbox }

func (s *Session) DegradationState() *types.FSDegradationState { return s.degradation }

func (s *Session) HasValidCredentials() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.closed && s.stored.RefreshToken != ""
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.tokens.ClearConnection(s.connectionID)
	return nil
}

func (s *Session) CreateAdapter(rootFolder types.Folder) (types.FSAdapter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, fmt.Errorf("dropbox session closed")
	}
	ctx := parseDropboxContext(rootFolder)
	return &DropboxFS{
		session: s,
		root:    rootFolder,
		ctx:     ctx,
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

func (s *Session) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.stored.ClientID,
		ClientSecret: s.stored.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://www.dropbox.com/oauth2/authorize",
			TokenURL: dropboxTokenURL,
		},
		Scopes: s.stored.Scopes,
	}
}

func (s *Session) refreshToken(ctx context.Context) (string, time.Time, error) {
	cfg := s.oauthConfig()
	src := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: s.stored.RefreshToken})
	tok, err := src.Token()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("dropbox refresh: %w", err)
	}
	if tok.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("dropbox refresh: empty access token")
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
		return nil, fmt.Errorf("dropbox: no access token after refresh")
	}
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"})
	return oauth2.NewClient(ctx, src), nil
}

func (s *Session) apiClient(ctx context.Context, namespaceID string) (*Client, error) {
	httpClient, err := s.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	return newClient(httpClient, namespaceID), nil
}

func (s *Session) listRoots(ctx context.Context) ([]cloud.Root, error) {
	client, err := s.apiClient(ctx, "")
	if err != nil {
		return nil, err
	}
	acct, err := client.getCurrentAccount(ctx)
	if err != nil {
		return nil, err
	}
	homeNS := acct.RootInfo.HomeNamespaceID
	display := "My Dropbox"
	if acct.Name.DisplayName != "" {
		display = acct.Name.DisplayName + "'s Dropbox"
	}
	roots := []cloud.Root{{
		ID:          "root",
		DriveID:     homeNS,
		DisplayName: display,
		RootType:    cloud.RootTypeUserRoot,
	}}
	rootNS := acct.RootInfo.RootNamespaceID
	if rootNS != "" && rootNS != homeNS {
		roots = append(roots, cloud.Root{
			ID:          "root",
			DriveID:     rootNS,
			DisplayName: "Team space",
			RootType:    cloud.RootTypeTeamSpace,
		})
	}
	if teamFolders, err := client.listTeamFolders(ctx); err == nil {
		for _, tf := range teamFolders {
			ns, nsErr := client.teamFolderNamespace(ctx, tf.TeamFolderID)
			if nsErr != nil {
				continue
			}
			roots = append(roots, cloud.Root{
				ID:          tf.TeamFolderID,
				DisplayName: tf.Name,
				RootType:    cloud.RootTypeTeamFolder,
				DriveID:     ns,
			})
		}
	}
	if shared, err := client.listSharedFolders(ctx); err == nil {
		seen := make(map[string]struct{})
		for _, sf := range shared {
			if sf.SharedFolderID == "" || sf.Name == "" {
				continue
			}
			if _, ok := seen[sf.SharedFolderID]; ok {
				continue
			}
			seen[sf.SharedFolderID] = struct{}{}
			root := cloud.Root{
				ID:          sf.SharedFolderID,
				DisplayName: sf.Name,
				RootType:    cloud.RootTypeSharedFolder,
			}
			if sf.PathLower != "" {
				root.DriveID = sf.PathLower
			}
			roots = append(roots, root)
		}
	}
	return roots, nil
}

type dropboxContext struct {
	RootType    string
	NamespaceID string
	RootPath    string
	FolderRef   string
}

func parseDropboxContext(folder types.Folder) dropboxContext {
	ctx := dropboxContext{RootType: cloud.RootTypeUserRoot}
	switch folder.Type {
	case cloud.RootTypeUserRoot:
		ctx.NamespaceID = folder.ParentId
		if ctx.NamespaceID == "" && folder.ServiceID != "" && folder.ServiceID != "root" {
			ctx.NamespaceID = folder.ServiceID
		}
	case cloud.RootTypeTeamSpace:
		ctx.RootType = cloud.RootTypeTeamSpace
		ctx.NamespaceID = folder.ParentId
		if ctx.NamespaceID == "" && folder.ServiceID != "" && folder.ServiceID != "root" {
			ctx.NamespaceID = folder.ServiceID
		}
	case cloud.RootTypeTeamFolder:
		ctx.RootType = cloud.RootTypeTeamFolder
		ctx.NamespaceID = folder.ParentId
		ctx.RootPath = ""
	case cloud.RootTypeSharedFolder:
		ctx.RootType = cloud.RootTypeSharedFolder
		if folder.ParentId != "" && folder.ParentId[0] == '/' {
			ctx.RootPath = folder.ParentId
		} else {
			ctx.NamespaceID = folder.ParentId
		}
	default:
		if folder.Type != "" && folder.Type != types.NodeTypeFolder {
			ctx.RootType = folder.Type
		}
	}
	if folder.ServiceID != "" && !isVirtualRootType(folder.Type) {
		ctx.FolderRef = folder.ServiceID
	} else if isVirtualRootType(folder.Type) && folder.ServiceID != "" && folder.ServiceID != folder.ParentId {
		ctx.FolderRef = folder.ServiceID
	}
	return ctx
}

func isVirtualRootType(t string) bool {
	switch t {
	case cloud.RootTypeUserRoot, cloud.RootTypeTeamSpace, cloud.RootTypeTeamFolder, cloud.RootTypeSharedFolder:
		return true
	default:
		return false
	}
}

// PrimeAccessToken stores a UI-supplied access token in memory only.
func (s *Session) PrimeAccessToken(accessToken string, expiresInSec int64) {
	expiry := time.Time{}
	if expiresInSec > 0 {
		expiry = time.Now().Add(time.Duration(expiresInSec) * time.Second)
	}
	s.tokens.SetAccessToken(s.connectionID, accessToken, expiry)
}

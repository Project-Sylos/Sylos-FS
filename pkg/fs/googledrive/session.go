// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func init() {
	cloud.RegisterFactory(factory{})
}

type factory struct{}

func (factory) ProviderID() string { return cloud.ProviderGoogleDrive }

func (factory) NewSession(connectionID string, stored cloud.StoredCredentials, tokens *cloud.TokenStore, degradation *types.FSDegradationState) (cloud.Session, error) {
	if stored.RefreshToken == "" {
		return nil, fmt.Errorf("google drive: refresh token required")
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
		return nil, fmt.Errorf("google drive: invalid session type")
	}
	srv, err := s.driveService(ctx)
	if err != nil {
		return nil, err
	}

	roots := []cloud.Root{
		{ID: "root", DisplayName: "My Drive", RootType: cloud.RootTypeMyDrive},
		{ID: "sharedWithMe", DisplayName: "Shared with me", RootType: cloud.RootTypeSharedWithMe},
	}

	pageToken := ""
	for {
		resp, err := srv.Drives.List().PageSize(100).PageToken(pageToken).Fields("nextPageToken,drives(id,name)").Do()
		if err != nil {
			return nil, err
		}
		for _, d := range resp.Drives {
			roots = append(roots, cloud.Root{
				ID:          d.Id,
				DisplayName: d.Name,
				RootType:    cloud.RootTypeSharedDrive,
				DriveID:     d.Id,
			})
		}
		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return roots, nil
}

// Session holds Google Drive OAuth state for one connection.
type Session struct {
	mu           sync.RWMutex
	connectionID string
	stored       cloud.StoredCredentials
	tokens       *cloud.TokenStore
	degradation  *types.FSDegradationState
	closed       bool
}

func (s *Session) ConnectionID() string { return s.connectionID }
func (s *Session) ProviderID() string   { return cloud.ProviderGoogleDrive }

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
		return nil, fmt.Errorf("google drive session closed")
	}
	ctx := parseDriveContext(rootFolder)
	return &DriveFS{
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
	endpoint := google.Endpoint
	return &oauth2.Config{
		ClientID:     s.stored.ClientID,
		ClientSecret: s.stored.ClientSecret,
		Endpoint:     endpoint,
		Scopes:       s.stored.Scopes,
	}
}

func (s *Session) refreshToken(ctx context.Context) (string, time.Time, error) {
	cfg := s.oauthConfig()
	src := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: s.stored.RefreshToken})
	tok, err := src.Token()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("google drive refresh: %w", err)
	}
	if tok.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("google drive refresh: empty access token")
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
		return nil, fmt.Errorf("google drive: no access token after refresh")
	}
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"})
	return oauth2.NewClient(ctx, src), nil
}

func (s *Session) driveService(ctx context.Context) (*drive.Service, error) {
	client, err := s.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	return drive.NewService(ctx, option.WithHTTPClient(client))
}

type driveContext struct {
	RootType string
	DriveID  string
	FolderID string
}

func parseDriveContext(folder types.Folder) driveContext {
	ctx := driveContext{FolderID: folder.ServiceID}
	if ctx.FolderID == "" {
		ctx.FolderID = "root"
	}
	switch folder.Type {
	case cloud.RootTypeSharedDrive:
		ctx.RootType = cloud.RootTypeSharedDrive
		ctx.DriveID = folder.ParentId
		if ctx.DriveID == "" {
			ctx.DriveID = folder.ServiceID
			ctx.FolderID = "root"
		}
	case cloud.RootTypeSharedWithMe:
		ctx.RootType = cloud.RootTypeSharedWithMe
		ctx.FolderID = folder.ServiceID
	case cloud.RootTypeMyDrive, "":
		ctx.RootType = cloud.RootTypeMyDrive
	default:
		if folder.Type != "" && folder.Type != types.NodeTypeFolder {
			ctx.RootType = folder.Type
		} else {
			ctx.RootType = cloud.RootTypeMyDrive
		}
	}
	return ctx
}

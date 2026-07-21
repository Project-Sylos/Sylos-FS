// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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
func (factory) ForbiddenMigrationRootIDs() []string {
	// Team space is a container of member folders / team folders, not a writable parent.
	return []string{"teamSpace"}
}

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
	// selectUser is Dropbox-API-Select-User for Business team-linked tokens.
	selectUser string
}

func (s *Session) ConnectionID() string { return s.connectionID }
func (s *Session) ProviderID() string   { return cloud.ProviderDropbox }

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
		return cloud.StoredCredentials{}, fmt.Errorf("dropbox session closed")
	}
	if s.stored.RefreshToken == "" {
		return cloud.StoredCredentials{}, fmt.Errorf("dropbox: no refresh token to export")
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
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("dropbox session closed")
	}
	dbxCtx := parseDropboxContext(rootFolder)
	dbxCtx, err := s.normalizeSharedFolderContext(context.Background(), dbxCtx)
	if err != nil {
		return nil, err
	}
	return &DropboxFS{
		session: s,
		root:    rootFolder,
		ctx:     dbxCtx,
	}, nil
}

// normalizeSharedFolderContext re-roots adapters onto a shared/team folder namespace when
// the selected folder is itself a Dropbox shared folder (Path-Root must be that namespace).
// For a nested folder under home/team space, it resolves path_lower once into RootPath so
// creates can join migration-relative paths without per-call get_metadata.
func (s *Session) normalizeSharedFolderContext(ctx context.Context, dbxCtx dropboxContext) (dropboxContext, error) {
	ref := strings.TrimSpace(dbxCtx.FolderRef)
	if ref == "" {
		return dbxCtx, nil
	}
	client, err := s.apiClient(ctx, strings.TrimSpace(dbxCtx.NamespaceID))
	if err != nil {
		return dbxCtx, fmt.Errorf("dropbox: resolve migration root: %w", err)
	}
	meta, err := client.getMetadata(ctx, dropboxPathRef(ref))
	if err == nil {
		if ns := strings.TrimSpace(meta.SharedFolderID); ns != "" {
			dbxCtx.RootType = cloud.RootTypeTeamFolder
			dbxCtx.NamespaceID = ns
			dbxCtx.FolderRef = ""
			dbxCtx.RootPath = ""
			return dbxCtx, nil
		}
		base := strings.TrimSpace(meta.PathLower)
		if base == "" {
			base = strings.TrimSpace(meta.PathDisplay)
		}
		if base == "" || !strings.HasPrefix(base, "/") {
			return dbxCtx, fmt.Errorf("dropbox: migration root %q has no path; cannot build create paths without get_metadata on every write", ref)
		}
		dbxCtx.RootPath = base
		dbxCtx.FolderRef = ""
		return dbxCtx, nil
	}
	// Selected id may already be a shared_folder_id / team_folder_id namespace.
	bare := strings.TrimPrefix(ref, "id:")
	bare = strings.TrimPrefix(bare, "ns:")
	if bare == "" || strings.HasPrefix(bare, "/") || bare == dbxCtx.NamespaceID {
		return dbxCtx, fmt.Errorf("dropbox: resolve migration root %q: %w", ref, err)
	}
	probe, probeErr := s.apiClient(ctx, bare)
	if probeErr != nil {
		return dbxCtx, fmt.Errorf("dropbox: resolve migration root %q: %w", ref, err)
	}
	if _, _, _, listErr := probe.listFolderPage(ctx, "", ""); listErr != nil {
		return dbxCtx, fmt.Errorf("dropbox: resolve migration root %q: %w", ref, err)
	}
	dbxCtx.RootType = cloud.RootTypeTeamFolder
	dbxCtx.NamespaceID = bare
	dbxCtx.FolderRef = ""
	dbxCtx.RootPath = ""
	return dbxCtx, nil
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
	if err := s.ensureMemberContext(ctx); err != nil {
		return nil, err
	}
	httpClient, err := s.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	selectUser := s.selectUser
	s.mu.RUnlock()
	return newClient(httpClient, namespaceID, selectUser), nil
}

// ensureMemberContext makes user endpoints work for Business team-linked OAuth tokens
// by resolving the authenticated admin and setting Dropbox-API-Select-User.
func (s *Session) ensureMemberContext(ctx context.Context) error {
	s.mu.RLock()
	if s.selectUser != "" || s.closed {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	httpClient, err := s.httpClient(ctx)
	if err != nil {
		return err
	}
	probe := newClient(httpClient, "", "")
	_, err = probe.getCurrentAccount(ctx)
	if err == nil {
		return nil
	}
	if !isTeamLinkedTokenError(err) {
		return err
	}
	admin, adminErr := probe.getAuthenticatedAdmin(ctx)
	if adminErr != nil {
		return fmt.Errorf("dropbox: team token requires member context: %w (get_current_account: %v)", adminErr, err)
	}
	memberID := strings.TrimSpace(admin.AdminProfile.TeamMemberID)
	if memberID == "" {
		return fmt.Errorf("dropbox: team token get_authenticated_admin returned empty team_member_id")
	}
	s.mu.Lock()
	s.selectUser = memberID
	s.mu.Unlock()
	return nil
}

func (s *Session) ResolveAccountIdentity(ctx context.Context) (cloud.AccountIdentity, error) {
	client, err := s.apiClient(ctx, "")
	if err != nil {
		return cloud.AccountIdentity{}, err
	}
	acct, err := client.getCurrentAccount(ctx)
	if err != nil {
		return cloud.AccountIdentity{}, err
	}
	return cloud.AccountIdentity{
		Email:       acct.Email,
		DisplayName: acct.Name.DisplayName,
	}, nil
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
			ID:          "teamSpace",
			DriveID:     rootNS,
			DisplayName: "Team space",
			RootType:    cloud.RootTypeTeamSpace,
		})
	}
	seenNS := make(map[string]struct{})
	teamClient := client
	s.mu.RLock()
	selectUser := s.selectUser
	s.mu.RUnlock()
	if selectUser != "" {
		// Team endpoints reject Select-User; use Select-Admin for the same member.
		teamClient = client.withSelectAdmin(selectUser)
	}
	if teamFolders, err := teamClient.listTeamFolders(ctx); err == nil {
		for _, tf := range teamFolders {
			ns, nsErr := teamClient.teamFolderNamespace(ctx, tf.TeamFolderID)
			if nsErr != nil {
				continue
			}
			seenNS[ns] = struct{}{}
			roots = append(roots, cloud.Root{
				ID:          tf.TeamFolderID,
				DisplayName: tf.Name,
				RootType:    cloud.RootTypeTeamFolder,
				DriveID:     ns,
			})
		}
	}
	if shared, err := client.listSharedFolders(ctx); err == nil {
		for _, sf := range shared {
			if sf.SharedFolderID == "" || sf.Name == "" {
				continue
			}
			if _, ok := seenNS[sf.SharedFolderID]; ok {
				continue
			}
			seenNS[sf.SharedFolderID] = struct{}{}
			// Always Path-Root by shared_folder_id. path_lower is often missing or only
			// valid under the team space, so using it under the home root yields path/not_found.
			roots = append(roots, cloud.Root{
				ID:          sf.SharedFolderID,
				DisplayName: sf.Name,
				RootType:    cloud.RootTypeSharedFolder,
				DriveID:     sf.SharedFolderID,
			})
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
		if ctx.NamespaceID == "" && folder.ServiceID != "" && folder.ServiceID != "root" && folder.ServiceID != "teamSpace" {
			ctx.NamespaceID = folder.ServiceID
		}
	case cloud.RootTypeTeamFolder:
		ctx.RootType = cloud.RootTypeTeamFolder
		ctx.NamespaceID = folder.ParentId
		if ctx.NamespaceID == "" {
			ctx.NamespaceID = folder.ServiceID
		}
		ctx.RootPath = ""
	case cloud.RootTypeSharedFolder:
		ctx.RootType = cloud.RootTypeSharedFolder
		if folder.ParentId != "" && folder.ParentId[0] == '/' {
			ctx.RootPath = folder.ParentId
		} else if folder.ParentId != "" {
			ctx.NamespaceID = folder.ParentId
		} else {
			// shared_folder_id is the namespace when path_lower was unavailable.
			ctx.NamespaceID = folder.ServiceID
		}
	default:
		// Listed shared/team mounts keep Type=folder but ServiceID==ParentId==namespace.
		if ns := sharedOrTeamNamespaceID(folder); ns != "" {
			ctx.RootType = cloud.RootTypeTeamFolder
			ctx.NamespaceID = ns
			ctx.RootPath = ""
			break
		}
		if folder.Type != "" && folder.Type != types.NodeTypeFolder {
			ctx.RootType = folder.Type
		}
		if folder.ParentId != "" {
			if folder.ParentId[0] == '/' {
				ctx.RootPath = folder.ParentId
			} else {
				ctx.NamespaceID = folder.ParentId
			}
		}
	}
	if folder.ServiceID != "" && !isVirtualRootType(folder.Type) && sharedOrTeamNamespaceID(folder) == "" {
		ctx.FolderRef = folder.ServiceID
	} else if isVirtualRootType(folder.Type) && folder.ServiceID != "" && folder.ServiceID != folder.ParentId &&
		folder.ServiceID != "root" && folder.ServiceID != "teamSpace" {
		ctx.FolderRef = folder.ServiceID
	}
	return ctx
}

// sharedOrTeamNamespaceID detects a Dropbox shared/team folder mount encoded as
// ServiceID == ParentId == namespace id (structural Type remains "folder").
func sharedOrTeamNamespaceID(folder types.Folder) string {
	ns := strings.TrimSpace(folder.ParentId)
	if ns == "" || ns[0] == '/' || strings.HasPrefix(ns, "id:") {
		return ""
	}
	if strings.TrimSpace(folder.ServiceID) != ns {
		return ""
	}
	return ns
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

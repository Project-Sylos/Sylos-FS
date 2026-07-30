// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	apiHost     = "https://api.dropboxapi.com"
	contentHost = "https://content.dropboxapi.com"
)

// APIError is a structured Dropbox RPC or content API error.
type APIError struct {
	Status       int
	ErrorSummary string
	ErrorTag     string
	RetryAfter   float64
	Body         []byte
}

func (e *APIError) Error() string {
	if e == nil {
		return "dropbox: unknown error"
	}
	if e.ErrorSummary != "" {
		return fmt.Sprintf("dropbox: %s (HTTP %d)", e.ErrorSummary, e.Status)
	}
	if len(e.Body) > 0 {
		return fmt.Sprintf("dropbox: HTTP %d tag=%q body=%s", e.Status, e.ErrorTag, truncate(string(e.Body), 200))
	}
	return fmt.Sprintf("dropbox: HTTP %d tag=%q", e.Status, e.ErrorTag)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type apiErrorBody struct {
	ErrorSummary string          `json:"error_summary"`
	Error        json.RawMessage `json:"error"`
}

type taggedError struct {
	Tag string `json:".tag"`
}

type rateLimitError struct {
	Tag        string  `json:".tag"`
	RetryAfter float64 `json:"retry_after"`
}

// Client performs Dropbox API v2 RPC and content calls for one namespace/member context.
type Client struct {
	httpClient  *http.Client
	pathRoot    string // Dropbox-API-Path-Root namespace_id
	selectUser  string // Dropbox-API-Select-User (team member id)
	selectAdmin string // Dropbox-API-Select-Admin (team member id)
}

func newClient(httpClient *http.Client, pathRoot, selectUser string) *Client {
	return &Client{httpClient: httpClient, pathRoot: pathRoot, selectUser: selectUser}
}

func (c *Client) withSelectAdmin(adminMemberID string) *Client {
	if c == nil {
		return nil
	}
	out := *c
	out.selectAdmin = strings.TrimSpace(adminMemberID)
	out.selectUser = ""
	return &out
}

func (c *Client) rpc(ctx context.Context, endpoint string, reqBody any, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	} else {
		// Dropbox RPC endpoints with no args require the JSON literal null, not an empty body.
		body = bytes.NewReader([]byte("null"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiHost+"/2/"+endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return parseAPIError(resp.StatusCode, raw, resp.Header)
	}
	if respBody == nil {
		return nil
	}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, respBody)
}

func (c *Client) content(ctx context.Context, endpoint string, apiArg any, body io.Reader, respBody any) ([]byte, error) {
	argJSON, err := json.Marshal(apiArg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, contentHost+"/2/"+endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Dropbox-API-Arg", string(argJSON))
	c.applyHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, parseAPIError(resp.StatusCode, raw, resp.Header)
	}
	if respBody != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, respBody); err != nil {
			return raw, err
		}
	}
	return raw, nil
}

func (c *Client) applyHeaders(req *http.Request) {
	if c.pathRoot != "" {
		root := fmt.Sprintf(`{".tag":"namespace_id","namespace_id":%q}`, c.pathRoot)
		req.Header.Set("Dropbox-API-Path-Root", root)
	}
	if c.selectUser != "" {
		req.Header.Set("Dropbox-API-Select-User", c.selectUser)
	}
	if c.selectAdmin != "" {
		req.Header.Set("Dropbox-API-Select-Admin", c.selectAdmin)
	}
}

func parseAPIError(status int, raw []byte, header http.Header) *APIError {
	err := &APIError{Status: status, Body: raw}
	var body apiErrorBody
	if json.Unmarshal(raw, &body) == nil {
		err.ErrorSummary = body.ErrorSummary
		if len(body.Error) > 0 {
			err.ErrorTag = extractDropboxErrorTag(body.Error, body.ErrorSummary)
			var rl rateLimitError
			if json.Unmarshal(body.Error, &rl) == nil && rl.RetryAfter > 0 {
				err.RetryAfter = rl.RetryAfter
			}
		}
	}
	if err.RetryAfter <= 0 {
		if v := header.Get("Retry-After"); v != "" {
			var sec float64
			if _, scanErr := fmt.Sscanf(v, "%f", &sec); scanErr == nil && sec > 0 {
				err.RetryAfter = sec
			}
		}
	}
	return err
}

type accountRootInfo struct {
	Tag              string `json:".tag"`
	RootNamespaceID  string `json:"root_namespace_id"`
	HomeNamespaceID  string `json:"home_namespace_id"`
}

type currentAccount struct {
	Email string `json:"email"`
	Name  struct {
		DisplayName string `json:"display_name"`
	} `json:"name"`
	RootInfo accountRootInfo `json:"root_info"`
}

func (c *Client) getCurrentAccount(ctx context.Context) (currentAccount, error) {
	var out currentAccount
	err := c.rpc(ctx, "users/get_current_account", nil, &out)
	return out, err
}

type spaceUsage struct {
	Used       int64           `json:"used"`
	Allocation json.RawMessage `json:"allocation"`
}

// getSpaceUsage returns Dropbox account storage usage (users/get_space_usage).
func (c *Client) getSpaceUsage(ctx context.Context) (used, allocated int64, err error) {
	var out spaceUsage
	if err := c.rpc(ctx, "users/get_space_usage", nil, &out); err != nil {
		return 0, 0, err
	}
	var alloc struct {
		Allocated int64 `json:"allocated"`
	}
	if len(out.Allocation) > 0 {
		_ = json.Unmarshal(out.Allocation, &alloc)
	}
	return out.Used, alloc.Allocated, nil
}

type authenticatedAdminResult struct {
	AdminProfile struct {
		TeamMemberID string `json:"team_member_id"`
		Email        string `json:"email"`
		Name         struct {
			DisplayName string `json:"display_name"`
		} `json:"name"`
	} `json:"admin_profile"`
}

// getAuthenticatedAdmin resolves the admin member for a Business team-linked token.
// Must be called without Select-User / Select-Admin headers.
func (c *Client) getAuthenticatedAdmin(ctx context.Context) (authenticatedAdminResult, error) {
	base := &Client{httpClient: c.httpClient}
	var out authenticatedAdminResult
	err := base.rpc(ctx, "team/token/get_authenticated_admin", nil, &out)
	return out, err
}

func isTeamLinkedTokenError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "entire dropbox business team") ||
		strings.Contains(msg, "team-linked") ||
		strings.Contains(msg, "dropbox business team")
}

type listFolderArg struct {
	Path                   string `json:"path"`
	Recursive              bool   `json:"recursive"`
	IncludeMountedFolders    bool   `json:"include_mounted_folders"`
	Limit                  uint32 `json:"limit,omitempty"`
}

type metadataTag struct {
	Tag string `json:".tag"`
}

type fileMetadata struct {
	Tag            string `json:".tag"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	PathDisplay    string `json:"path_display"`
	PathLower      string `json:"path_lower"`
	Size           uint64 `json:"size"`
	ClientMod      string `json:"client_modified"`
	ServerMod      string `json:"server_modified"`
	SharedFolderID string `json:"shared_folder_id"`
}

type listFolderResult struct {
	Entries []json.RawMessage `json:"entries"`
	Cursor  string            `json:"cursor"`
	HasMore bool              `json:"has_more"`
}

func (c *Client) listFolderAll(ctx context.Context, path string) ([]fileMetadata, error) {
	var all []fileMetadata
	var cursor string
	first := true
	for first || cursor != "" {
		var batch []fileMetadata
		var next string
		var hasMore bool
		var err error
		if first {
			batch, next, hasMore, err = c.listFolderPage(ctx, path, "")
		} else {
			batch, next, hasMore, err = c.listFolderContinue(ctx, cursor)
		}
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		cursor = next
		first = false
		if !hasMore {
			break
		}
	}
	return all, nil
}

func (c *Client) listFolderPage(ctx context.Context, path string, cursor string) ([]fileMetadata, string, bool, error) {
	if cursor != "" {
		return c.listFolderContinue(ctx, cursor)
	}
	var resp listFolderResult
	err := c.rpc(ctx, "files/list_folder", listFolderArg{
		Path:                path,
		Recursive:           false,
		IncludeMountedFolders: true,
		Limit:               1000,
	}, &resp)
	if err != nil {
		return nil, "", false, err
	}
	return decodeEntries(resp.Entries), resp.Cursor, resp.HasMore, nil
}

func (c *Client) listFolderContinue(ctx context.Context, cursor string) ([]fileMetadata, string, bool, error) {
	var resp listFolderResult
	err := c.rpc(ctx, "files/list_folder/continue", map[string]string{"cursor": cursor}, &resp)
	if err != nil {
		return nil, "", false, err
	}
	return decodeEntries(resp.Entries), resp.Cursor, resp.HasMore, nil
}

func decodeEntries(raw []json.RawMessage) []fileMetadata {
	out := make([]fileMetadata, 0, len(raw))
	for _, r := range raw {
		var tag metadataTag
		if json.Unmarshal(r, &tag) != nil {
			continue
		}
		if tag.Tag != "folder" && tag.Tag != "file" {
			continue
		}
		var meta fileMetadata
		if json.Unmarshal(r, &meta) == nil {
			out = append(out, meta)
		}
	}
	return out
}

type createFolderArg struct {
	Path      string `json:"path"`
	Autorename bool  `json:"autorename"`
}

type createFolderResult struct {
	Metadata fileMetadata `json:"metadata"`
}

func (c *Client) createFolder(ctx context.Context, path string) (fileMetadata, error) {
	var resp createFolderResult
	err := c.rpc(ctx, "files/create_folder_v2", createFolderArg{Path: path, Autorename: false}, &resp)
	return resp.Metadata, err
}

type deleteArg struct {
	Path string `json:"path"`
}

func (c *Client) deleteEntry(ctx context.Context, path string) error {
	return c.rpc(ctx, "files/delete_v2", deleteArg{Path: path}, nil)
}

func (c *Client) moveEntry(ctx context.Context, fromPath, toPath string) (fileMetadata, error) {
	var resp struct {
		Metadata fileMetadata `json:"metadata"`
	}
	err := c.rpc(ctx, "files/move_v2", map[string]any{
		"from_path":  fromPath,
		"to_path":    toPath,
		"autorename": false,
	}, &resp)
	return resp.Metadata, err
}

func (c *Client) download(ctx context.Context, path string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, contentHost+"/2/files/download", nil)
	if err != nil {
		return nil, err
	}
	argJSON, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return nil, err
	}
	req.Header.Set("Dropbox-API-Arg", string(argJSON))
	req.Header.Set("Content-Type", "application/octet-stream")
	c.applyHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, parseAPIError(resp.StatusCode, raw, resp.Header)
	}
	return resp.Body, nil
}

type uploadArg struct {
	Path       string `json:"path"`
	Mode       string `json:"mode"`
	Autorename bool   `json:"autorename"`
	Mute       bool   `json:"mute"`
}

func (c *Client) upload(ctx context.Context, path string, body io.Reader) (fileMetadata, error) {
	var meta fileMetadata
	_, err := c.content(ctx, "files/upload", uploadArg{
		Path:       path,
		Mode:       "overwrite",
		Autorename: false,
		Mute:       false,
	}, body, &meta)
	return meta, err
}

const uploadSessionChunk = 16 * 1024 * 1024

func (c *Client) uploadSession(ctx context.Context, path string, body io.Reader) (fileMetadata, error) {
	var startResp struct {
		SessionID string `json:"session_id"`
	}
	if _, err := c.content(ctx, "files/upload_session/start", struct{}{}, nil, &startResp); err != nil {
		return fileMetadata{}, err
	}
	offset, err := c.appendUploadSession(ctx, startResp.SessionID, body)
	if err != nil {
		return fileMetadata{}, err
	}
	var finishResp struct {
		Metadata fileMetadata `json:"metadata"`
	}
	finishArg := map[string]any{
		"cursor": map[string]any{
			"session_id": startResp.SessionID,
			"offset":     offset,
		},
		"commit": uploadArg{
			Path:       path,
			Mode:       "overwrite",
			Autorename: false,
			Mute:       false,
		},
	}
	if _, err := c.content(ctx, "files/upload_session/finish", finishArg, nil, &finishResp); err != nil {
		return fileMetadata{}, err
	}
	return finishResp.Metadata, nil
}

// appendUploadSession streams body into an open upload session and closes it on the last chunk.
// Empty bodies still send append_v2 with close=true so the session is finish-ready at offset 0.
func (c *Client) appendUploadSession(ctx context.Context, sessionID string, body io.Reader) (uint64, error) {
	offset := uint64(0)
	buf := make([]byte, uploadSessionChunk)
	closed := false
	for {
		n, readErr := io.ReadFull(body, buf)
		if readErr == io.ErrUnexpectedEOF {
			readErr = io.EOF
		}
		atEOF := readErr == io.EOF
		if n > 0 {
			chunk := buf[:n]
			appendArg := map[string]any{
				"cursor": map[string]any{
					"session_id": sessionID,
					"offset":     offset,
				},
				"close": atEOF,
			}
			if _, err := c.content(ctx, "files/upload_session/append_v2", appendArg, bytes.NewReader(chunk), nil); err != nil {
				return 0, err
			}
			offset += uint64(n)
			if atEOF {
				closed = true
			}
		}
		if atEOF {
			break
		}
		if readErr != nil {
			return 0, readErr
		}
	}
	if !closed {
		appendArg := map[string]any{
			"cursor": map[string]any{
				"session_id": sessionID,
				"offset":     offset,
			},
			"close": true,
		}
		if _, err := c.content(ctx, "files/upload_session/append_v2", appendArg, bytes.NewReader(nil), nil); err != nil {
			return 0, err
		}
	}
	return offset, nil
}

type teamFolderMetadata struct {
	TeamFolderID string `json:"team_folder_id"`
	Name         string `json:"name"`
	Status       struct {
		Tag string `json:".tag"`
	} `json:"status"`
}

type teamFolderListResult struct {
	TeamFolders []teamFolderMetadata `json:"team_folders"`
	Cursor      string               `json:"cursor"`
	HasMore     bool                 `json:"has_more"`
}

func (c *Client) listTeamFolders(ctx context.Context) ([]teamFolderMetadata, error) {
	var all []teamFolderMetadata
	cursor := ""
	for {
		var resp teamFolderListResult
		var err error
		if cursor == "" {
			err = c.rpc(ctx, "team/team_folder/list", map[string]any{"limit": 1000}, &resp)
		} else {
			err = c.rpc(ctx, "team/team_folder/list/continue", map[string]string{"cursor": cursor}, &resp)
		}
		if err != nil {
			return nil, err
		}
		for _, tf := range resp.TeamFolders {
			if tf.Status.Tag == "active" {
				all = append(all, tf)
			}
		}
		if !resp.HasMore {
			break
		}
		cursor = resp.Cursor
	}
	return all, nil
}

type sharedFolderMetadata struct {
	SharedFolderID string `json:"shared_folder_id"`
	Name           string `json:"name"`
	PathLower      string `json:"path_lower"`
}

type sharedFolderListResult struct {
	Entries []sharedFolderMetadata `json:"entries"`
	Cursor  string                 `json:"cursor"`
}

func (c *Client) listSharedFolders(ctx context.Context) ([]sharedFolderMetadata, error) {
	var all []sharedFolderMetadata
	cursor := ""
	for {
		var resp sharedFolderListResult
		var err error
		if cursor == "" {
			err = c.rpc(ctx, "sharing/list_folders", map[string]any{"limit": 1000}, &resp)
		} else {
			err = c.rpc(ctx, "sharing/list_folders/continue", map[string]string{"cursor": cursor}, &resp)
		}
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Entries...)
		if resp.Cursor == "" {
			break
		}
		cursor = resp.Cursor
	}
	return all, nil
}


// teamFolderNamespace returns the namespace id for Path-Root. For Dropbox team folders,
// team_folder_id is the shared-folder / namespace id.
func (c *Client) teamFolderNamespace(_ context.Context, teamFolderID string) (string, error) {
	id := strings.TrimSpace(teamFolderID)
	if id == "" {
		return "", fmt.Errorf("dropbox: empty team folder id")
	}
	return id, nil
}

func dropboxPathRef(idOrPath string) string {
	idOrPath = strings.TrimSpace(idOrPath)
	if idOrPath == "" {
		return ""
	}
	if strings.HasPrefix(idOrPath, "/") || strings.HasPrefix(idOrPath, "id:") {
		return idOrPath
	}
	return "id:" + idOrPath
}

func isDropboxRootRef(parentID string) bool {
	switch strings.TrimSpace(parentID) {
	case "", "root":
		return true
	default:
		return false
	}
}

func (c *Client) getMetadata(ctx context.Context, pathRef string) (fileMetadata, error) {
	var meta fileMetadata
	err := c.rpc(ctx, "files/get_metadata", map[string]any{
		"path":                 pathRef,
		"include_media_info":   false,
		"include_deleted":      false,
		"include_has_explicit_shared_members": false,
	}, &meta)
	return meta, err
}

func parentPathForCreate(parentID, name string) string {
	if isDropboxRootRef(parentID) {
		return "/" + name
	}
	ref := dropboxPathRef(parentID)
	if strings.HasPrefix(ref, "/") {
		if ref == "/" {
			return "/" + name
		}
		return ref + "/" + name
	}
	return ref + "/" + name
}

// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package msgraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	GraphBaseURL = "https://graph.microsoft.com/v1.0"
	TokenURL     = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	AuthURL      = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"

	// DefaultListPageSize is the Graph children page size.
	DefaultListPageSize = 200
	// UploadChunkSize must be a multiple of 320 KiB; 5 MiB is a common recommendation.
	UploadChunkSize = 5 * 1024 * 1024
	// SimpleUploadMaxBytes uses PUT .../content for small files.
	SimpleUploadMaxBytes = 4 * 1024 * 1024
)

// APIError is a structured Microsoft Graph HTTP error.
type APIError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
	Body       []byte
	Header     http.Header
}

func (e *APIError) Error() string {
	if e == nil {
		return "msgraph: unknown error"
	}
	if e.Code != "" || e.Message != "" {
		return fmt.Sprintf("msgraph: %s: %s (HTTP %d)", e.Code, e.Message, e.Status)
	}
	if len(e.Body) > 0 {
		return fmt.Sprintf("msgraph: HTTP %d body=%s", e.Status, truncate(string(e.Body), 200))
	}
	return fmt.Sprintf("msgraph: HTTP %d", e.Status)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type graphErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Client performs Microsoft Graph calls with a bearer-authenticated HTTP client.
type Client struct {
	HTTP *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{HTTP: httpClient}
}

func (c *Client) DoJSON(ctx context.Context, method, pathOrURL string, reqBody any, respBody any) error {
	_, err := c.do(ctx, method, pathOrURL, reqBody, respBody, true)
	return err
}

func (c *Client) DoRaw(ctx context.Context, method, pathOrURL string, body io.Reader, contentType string, headers map[string]string) (*http.Response, error) {
	fullURL := resolveURL(pathOrURL)
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.HTTP.Do(req)
}

func (c *Client) do(ctx context.Context, method, pathOrURL string, reqBody any, respBody any, closeBody bool) (*http.Response, error) {
	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	fullURL := resolveURL(pathOrURL)
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return nil, parseAPIError(resp.StatusCode, raw, resp.Header)
	}
	if respBody == nil {
		if closeBody {
			resp.Body.Close()
		}
		return resp, nil
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return resp, nil
	}
	if err := json.Unmarshal(raw, respBody); err != nil {
		return nil, fmt.Errorf("msgraph: decode response: %w", err)
	}
	return resp, nil
}

func resolveURL(pathOrURL string) string {
	if strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://") {
		return pathOrURL
	}
	if strings.HasPrefix(pathOrURL, "/") {
		return GraphBaseURL + pathOrURL
	}
	return GraphBaseURL + "/" + pathOrURL
}

func parseAPIError(status int, raw []byte, header http.Header) *APIError {
	err := &APIError{Status: status, Body: raw, Header: header.Clone()}
	var body graphErrorBody
	if json.Unmarshal(raw, &body) == nil {
		err.Code = body.Error.Code
		err.Message = body.Error.Message
	}
	err.RetryAfter = ParseRetryAfter(header)
	return err
}

// ParseRetryAfter reads Retry-After as seconds or HTTP-date.
func ParseRetryAfter(header http.Header) time.Duration {
	if header == nil {
		return 0
	}
	v := header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

// DriveItem is a subset of the Graph driveItem resource.
type DriveItem struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Size                 int64     `json:"size"`
	LastModifiedDateTime string    `json:"lastModifiedDateTime"`
	Folder               *struct{} `json:"folder,omitempty"`
	File                 *struct{} `json:"file,omitempty"`
	ParentReference      *struct {
		DriveID string `json:"driveId"`
		ID      string `json:"id"`
		Path    string `json:"path"`
	} `json:"parentReference,omitempty"`
	DownloadURL string `json:"@microsoft.graph.downloadUrl,omitempty"`
}

// Drive is a Graph drive (document library / OneDrive).
type Drive struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DriveType string `json:"driveType"`
	WebURL    string `json:"webUrl"`
}

// Site is a Graph site.
type Site struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DisplayName    string `json:"displayName"`
	WebURL         string `json:"webUrl"`
	IsPersonalSite bool   `json:"isPersonalSite"`
}

type listResponse[T any] struct {
	Value    []T    `json:"value"`
	NextLink string `json:"@odata.nextLink"`
}

// ListChildren lists drive item children, following pagination until exhausted or maxPages.
func (c *Client) ListChildren(ctx context.Context, itemPath string, pageSize int) ([]DriveItem, error) {
	if pageSize <= 0 {
		pageSize = DefaultListPageSize
	}
	path := itemPath
	if !strings.Contains(path, "?") {
		path = path + "?$top=" + strconv.Itoa(pageSize) +
			"&$select=id,name,size,lastModifiedDateTime,folder,file,parentReference"
	}
	var all []DriveItem
	for path != "" {
		var page listResponse[DriveItem]
		if err := c.DoJSON(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Value...)
		path = page.NextLink
	}
	return all, nil
}

// GetDrive returns drive metadata.
func (c *Client) GetDrive(ctx context.Context, drivePath string) (Drive, error) {
	var d Drive
	err := c.DoJSON(ctx, http.MethodGet, drivePath, nil, &d)
	return d, err
}

// GetItem returns a drive item.
func (c *Client) GetItem(ctx context.Context, itemPath string) (DriveItem, error) {
	var item DriveItem
	err := c.DoJSON(ctx, http.MethodGet, itemPath+"?$select=id,name,size,lastModifiedDateTime,folder,file,parentReference,@microsoft.graph.downloadUrl", nil, &item)
	return item, err
}

// CreateFolder creates a folder under parentPath (/drives/.../items/.../children or /me/drive/...).
func (c *Client) CreateFolder(ctx context.Context, parentChildrenPath, name string) (DriveItem, error) {
	body := map[string]any{
		"name":                              name,
		"folder":                            map[string]any{},
		"@microsoft.graph.conflictBehavior": "fail",
	}
	var item DriveItem
	err := c.DoJSON(ctx, http.MethodPost, parentChildrenPath, body, &item)
	return item, err
}

// DeleteItem deletes a drive item.
func (c *Client) DeleteItem(ctx context.Context, itemPath string) error {
	return c.DoJSON(ctx, http.MethodDelete, itemPath, nil, nil)
}

// OpenDownload returns a ReadCloser for item content (follows redirect / download URL).
func (c *Client) OpenDownload(ctx context.Context, itemPath string) (io.ReadCloser, error) {
	item, err := c.GetItem(ctx, itemPath)
	if err != nil {
		return nil, err
	}
	if item.DownloadURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.DownloadURL, nil)
		if err != nil {
			return nil, err
		}
		// Pre-authenticated download URLs must not send Authorization.
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			return nil, parseAPIError(resp.StatusCode, raw, resp.Header)
		}
		return resp.Body, nil
	}
	resp, err := c.DoRaw(ctx, http.MethodGet, itemPath+"/content", nil, "", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return nil, parseAPIError(resp.StatusCode, raw, resp.Header)
	}
	return resp.Body, nil
}

type uploadSession struct {
	UploadURL string `json:"uploadUrl"`
}

// CreateUploadSession starts a resumable upload session.
func (c *Client) CreateUploadSession(ctx context.Context, createPath string, body any) (string, error) {
	var sess uploadSession
	if err := c.DoJSON(ctx, http.MethodPost, createPath, body, &sess); err != nil {
		return "", err
	}
	if sess.UploadURL == "" {
		return "", fmt.Errorf("msgraph: empty uploadUrl")
	}
	return sess.UploadURL, nil
}

// PutContent uploads a small file with a single PUT.
func (c *Client) PutContent(ctx context.Context, contentPath string, body io.Reader, size int64) (DriveItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, resolveURL(contentPath), body)
	if err != nil {
		return DriveItem{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if size >= 0 {
		req.ContentLength = size
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return DriveItem{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return DriveItem{}, err
	}
	if resp.StatusCode >= 400 {
		return DriveItem{}, parseAPIError(resp.StatusCode, raw, resp.Header)
	}
	var item DriveItem
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &item); err != nil {
			return DriveItem{}, err
		}
	}
	return item, nil
}

// UploadSessionChunks uploads via an existing upload session URL.
func (c *Client) UploadSessionChunks(ctx context.Context, uploadURL string, r io.ReaderAt, size int64) (DriveItem, error) {
	if size < 0 {
		return DriveItem{}, fmt.Errorf("msgraph: upload size required")
	}
	var item DriveItem
	offset := int64(0)
	buf := make([]byte, UploadChunkSize)
	for offset < size {
		chunk := UploadChunkSize
		remaining := size - offset
		if remaining < int64(chunk) {
			chunk = int(remaining)
		}
		n, err := r.ReadAt(buf[:chunk], offset)
		if err != nil && err != io.EOF {
			return DriveItem{}, err
		}
		if n == 0 {
			break
		}
		end := offset + int64(n) - 1
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(buf[:n]))
		if err != nil {
			return DriveItem{}, err
		}
		req.Header.Set("Content-Length", strconv.Itoa(n))
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, end, size))
		// Upload session URLs are pre-authenticated; use a client without Graph Authorization.
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return DriveItem{}, err
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return DriveItem{}, readErr
		}
		if resp.StatusCode >= 400 {
			return DriveItem{}, parseAPIError(resp.StatusCode, raw, resp.Header)
		}
		offset += int64(n)
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &item)
			}
			return item, nil
		}
	}
	return item, nil
}

// ListSitesSearch lists sites the signed-in user can access via search.
func (c *Client) ListSitesSearch(ctx context.Context, query string) ([]Site, error) {
	if query == "" {
		query = "*"
	}
	path := "/sites?search=" + url.QueryEscape(query) + "&$select=id,name,displayName,webUrl,isPersonalSite"
	var all []Site
	for path != "" {
		var page listResponse[Site]
		if err := c.DoJSON(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Value...)
		path = page.NextLink
	}
	return all, nil
}

// ListSiteDrives lists document libraries under a site.
func (c *Client) ListSiteDrives(ctx context.Context, siteID string) ([]Drive, error) {
	path := "/sites/" + url.PathEscape(siteID) + "/drives?$select=id,name,driveType,webUrl"
	var all []Drive
	for path != "" {
		var page listResponse[Drive]
		if err := c.DoJSON(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Value...)
		path = page.NextLink
	}
	return all, nil
}

// ListSubsites lists subsites under a site.
func (c *Client) ListSubsites(ctx context.Context, siteID string) ([]Site, error) {
	path := "/sites/" + url.PathEscape(siteID) + "/sites?$select=id,name,displayName,webUrl,isPersonalSite"
	var all []Site
	for path != "" {
		var page listResponse[Site]
		if err := c.DoJSON(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Value...)
		path = page.NextLink
	}
	return all, nil
}

// SharedWithMe lists items shared with the signed-in user.
func (c *Client) SharedWithMe(ctx context.Context) ([]DriveItem, error) {
	path := "/me/drive/sharedWithMe?$select=id,name,size,lastModifiedDateTime,folder,file,parentReference"
	var all []DriveItem
	for path != "" {
		var page listResponse[DriveItem]
		if err := c.DoJSON(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Value...)
		path = page.NextLink
	}
	return all, nil
}

// Me returns basic profile for account identity.
func (c *Client) Me(ctx context.Context) (email, displayName string, err error) {
	var me struct {
		DisplayName       string `json:"displayName"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, "/me?$select=displayName,mail,userPrincipalName", nil, &me); err != nil {
		return "", "", err
	}
	email = me.Mail
	if email == "" {
		email = me.UserPrincipalName
	}
	return email, me.DisplayName, nil
}

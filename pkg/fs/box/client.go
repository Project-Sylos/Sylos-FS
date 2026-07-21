// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package box

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	apiHost    = "https://api.box.com/2.0"
	uploadHost = "https://upload.box.com/api/2.0"

	// SimpleUploadMaxBytes is the Box simple-upload limit (50 MiB).
	SimpleUploadMaxBytes = 50 * 1024 * 1024
	// DefaultListLimit is the page size for folder items (Box max 1000).
	DefaultListLimit = 1000
	itemFields       = "id,type,name,size,modified_at,sha1,parent"
)

// APIError is a structured Box API error.
type APIError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
	Body       []byte
}

func (e *APIError) Error() string {
	if e == nil {
		return "box: unknown error"
	}
	if e.Message != "" {
		return fmt.Sprintf("box: %s (HTTP %d code=%q)", e.Message, e.Status, e.Code)
	}
	if len(e.Body) > 0 {
		return fmt.Sprintf("box: HTTP %d code=%q body=%s", e.Status, e.Code, truncate(string(e.Body), 200))
	}
	return fmt.Sprintf("box: HTTP %d code=%q", e.Status, e.Code)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type boxErrorBody struct {
	Type    string `json:"type"`
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Name    string `json:"name"`
}

// Client performs Box API calls.
type Client struct {
	httpClient *http.Client
}

func newClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Login string `json:"login"`
}

type Item struct {
	ID         string `json:"id"`
	Type       string `json:"type"` // file | folder
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
	SHA1       string `json:"sha1"`
	Parent     *struct {
		ID string `json:"id"`
	} `json:"parent"`
}

type itemsResponse struct {
	Entries    []Item `json:"entries"`
	NextMarker string `json:"next_marker"`
	Limit      int    `json:"limit"`
}

type uploadSession struct {
	ID                 string `json:"id"`
	PartSize           int64  `json:"part_size"`
	TotalParts         int    `json:"total_parts"`
	NumPartsProcessed  int    `json:"num_parts_processed"`
	SessionEndpoints   struct {
		UploadPart string `json:"upload_part"`
		Commit     string `json:"commit"`
		Abort      string `json:"abort"`
	} `json:"session_endpoints"`
}

type uploadPart struct {
	PartID string `json:"part_id"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	SHA1   string `json:"sha1"`
}

func (c *Client) doJSON(ctx context.Context, method, rawURL string, reqBody any, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, respBody)
}

func (c *Client) do(req *http.Request, respBody any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(resp, raw)
	}
	if respBody == nil || len(raw) == 0 || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.Unmarshal(raw, respBody); err != nil {
		return fmt.Errorf("box: decode response: %w", err)
	}
	return nil
}

func parseAPIError(resp *http.Response, raw []byte) error {
	apiErr := &APIError{Status: resp.StatusCode, Body: raw}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
			apiErr.RetryAfter = time.Duration(secs) * time.Second
		}
	}
	var body boxErrorBody
	if json.Unmarshal(raw, &body) == nil {
		apiErr.Code = body.Code
		if apiErr.Code == "" {
			apiErr.Code = body.Name
		}
		apiErr.Message = body.Message
	}
	return apiErr
}

func (c *Client) GetCurrentUser(ctx context.Context) (User, error) {
	var user User
	err := c.doJSON(ctx, http.MethodGet, apiHost+"/users/me", nil, &user)
	return user, err
}

func (c *Client) ListFolderItems(ctx context.Context, folderID string, marker string, limit int) ([]Item, string, error) {
	if folderID == "" {
		folderID = rootFolderID
	}
	if limit <= 0 || limit > DefaultListLimit {
		limit = DefaultListLimit
	}
	q := url.Values{}
	q.Set("fields", itemFields)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("usemarker", "true")
	if marker != "" {
		q.Set("marker", marker)
	}
	rawURL := fmt.Sprintf("%s/folders/%s/items?%s", apiHost, url.PathEscape(folderID), q.Encode())
	var resp itemsResponse
	if err := c.doJSON(ctx, http.MethodGet, rawURL, nil, &resp); err != nil {
		return nil, "", err
	}
	return resp.Entries, resp.NextMarker, nil
}

func (c *Client) ListFolderItemsAll(ctx context.Context, folderID string) ([]Item, error) {
	var all []Item
	marker := ""
	for {
		entries, next, err := c.ListFolderItems(ctx, folderID, marker, DefaultListLimit)
		if err != nil {
			return nil, err
		}
		all = append(all, entries...)
		if next == "" {
			return all, nil
		}
		marker = next
	}
}

func (c *Client) CreateFolder(ctx context.Context, parentID, name string) (Item, error) {
	if parentID == "" {
		parentID = rootFolderID
	}
	var out Item
	err := c.doJSON(ctx, http.MethodPost, apiHost+"/folders", map[string]any{
		"name": name,
		"parent": map[string]string{
			"id": parentID,
		},
	}, &out)
	return out, err
}

func (c *Client) DeleteFile(ctx context.Context, fileID string) error {
	return c.doJSON(ctx, http.MethodDelete, apiHost+"/files/"+url.PathEscape(fileID), nil, nil)
}

func (c *Client) DeleteFolder(ctx context.Context, folderID string) error {
	rawURL := apiHost + "/folders/" + url.PathEscape(folderID) + "?recursive=true"
	return c.doJSON(ctx, http.MethodDelete, rawURL, nil, nil)
}

func (c *Client) DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiHost+"/files/"+url.PathEscape(fileID)+"/content", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, parseAPIError(resp, raw)
	}
	return resp.Body, nil
}

func (c *Client) UploadNewFile(ctx context.Context, parentID, name string, content io.Reader, size int64) (Item, error) {
	if parentID == "" {
		parentID = rootFolderID
	}
	attrs, err := json.Marshal(map[string]any{
		"name": name,
		"parent": map[string]string{
			"id": parentID,
		},
	})
	if err != nil {
		return Item{}, err
	}
	return c.multipartUpload(ctx, uploadHost+"/files/content", "attributes", string(attrs), content, size)
}

func (c *Client) UploadFileVersion(ctx context.Context, fileID string, content io.Reader, size int64) (Item, error) {
	return c.multipartUpload(ctx, uploadHost+"/files/"+url.PathEscape(fileID)+"/content", "", "", content, size)
}

func (c *Client) multipartUpload(ctx context.Context, rawURL, attrField, attrs string, content io.Reader, size int64) (Item, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		var writeErr error
		defer func() {
			_ = mw.Close()
			_ = pw.CloseWithError(writeErr)
		}()
		if attrField != "" {
			if err := mw.WriteField(attrField, attrs); err != nil {
				writeErr = err
				return
			}
		}
		part, err := mw.CreateFormFile("file", "content")
		if err != nil {
			writeErr = err
			return
		}
		if _, err := io.Copy(part, content); err != nil {
			writeErr = err
			return
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, pr)
	if err != nil {
		_ = pr.Close()
		return Item{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if size >= 0 {
		// Content-Length is unknown for multipart; leave unset.
		_ = size
	}

	var entries struct {
		Entries []Item `json:"entries"`
	}
	if err := c.do(req, &entries); err != nil {
		return Item{}, err
	}
	if len(entries.Entries) == 0 {
		return Item{}, fmt.Errorf("box: upload returned no entries")
	}
	return entries.Entries[0], nil
}

func (c *Client) CreateUploadSession(ctx context.Context, folderID, fileName string, fileSize int64) (uploadSession, error) {
	if folderID == "" {
		folderID = rootFolderID
	}
	var sess uploadSession
	err := c.doJSON(ctx, http.MethodPost, uploadHost+"/files/upload_sessions", map[string]any{
		"folder_id": folderID,
		"file_size": fileSize,
		"file_name": fileName,
	}, &sess)
	return sess, err
}

func (c *Client) CreateUploadSessionForFile(ctx context.Context, fileID string, fileSize int64) (uploadSession, error) {
	var sess uploadSession
	err := c.doJSON(ctx, http.MethodPost, uploadHost+"/files/"+url.PathEscape(fileID)+"/upload_sessions", map[string]any{
		"file_size": fileSize,
	}, &sess)
	return sess, err
}

func (c *Client) UploadSessionPart(ctx context.Context, uploadPartURL string, part []byte, offset, totalSize int64) (uploadPart, error) {
	sum := sha1.Sum(part)
	digest := "sha=" + base64.StdEncoding.EncodeToString(sum[:])
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadPartURL, bytes.NewReader(part))
	if err != nil {
		return uploadPart{}, err
	}
	end := offset + int64(len(part)) - 1
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Digest", digest)
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, end, totalSize))
	var resp struct {
		Part uploadPart `json:"part"`
	}
	if err := c.do(req, &resp); err != nil {
		return uploadPart{}, err
	}
	return resp.Part, nil
}

func (c *Client) CommitUploadSession(ctx context.Context, commitURL string, parts []uploadPart, contentSHA1 string) (Item, error) {
	digest := "sha=" + contentSHA1
	body := map[string]any{
		"parts": parts,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return Item{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, commitURL, bytes.NewReader(b))
	if err != nil {
		return Item{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Digest", digest)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Item{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Item{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Item{}, parseAPIError(resp, raw)
	}
	var entries struct {
		Entries []Item `json:"entries"`
	}
	if err := json.Unmarshal(raw, &entries); err == nil && len(entries.Entries) > 0 {
		return entries.Entries[0], nil
	}
	var item Item
	if err := json.Unmarshal(raw, &item); err == nil && item.ID != "" {
		return item, nil
	}
	return Item{}, fmt.Errorf("box: commit returned no file entries")
}

func (c *Client) UploadChunked(ctx context.Context, folderID, fileName, existingFileID string, reader io.ReaderAt, size int64) (Item, error) {
	var sess uploadSession
	var err error
	if existingFileID != "" {
		sess, err = c.CreateUploadSessionForFile(ctx, existingFileID, size)
	} else {
		sess, err = c.CreateUploadSession(ctx, folderID, fileName, size)
	}
	if err != nil {
		return Item{}, err
	}
	partSize := sess.PartSize
	if partSize <= 0 {
		partSize = 8 * 1024 * 1024
	}
	uploadURL := sess.SessionEndpoints.UploadPart
	if uploadURL == "" {
		uploadURL = uploadHost + "/files/upload_sessions/" + url.PathEscape(sess.ID)
	}
	commitURL := sess.SessionEndpoints.Commit
	if commitURL == "" {
		commitURL = uploadHost + "/files/upload_sessions/" + url.PathEscape(sess.ID) + "/commit"
	}

	h := sha1.New()
	var parts []uploadPart
	var offset int64
	for offset < size {
		n := int(partSize)
		if rem := size - offset; rem < int64(n) {
			n = int(rem)
		}
		section := make([]byte, n)
		if _, err := reader.ReadAt(section, offset); err != nil && err != io.EOF {
			return Item{}, err
		}
		if _, err := h.Write(section); err != nil {
			return Item{}, err
		}
		part, err := c.UploadSessionPart(ctx, uploadURL, section, offset, size)
		if err != nil {
			return Item{}, err
		}
		parts = append(parts, part)
		offset += int64(n)
	}
	contentSHA1 := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return c.CommitUploadSession(ctx, commitURL, parts, contentSHA1)
}

func parentIDOf(item Item) string {
	if item.Parent == nil {
		return ""
	}
	return strings.TrimSpace(item.Parent.ID)
}

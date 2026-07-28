package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	premiumizeBaseURL = "https://www.premiumize.me/api"
	// No documented per-key rate cap found (unlike Real-Debrid/TorBox) --
	// conservative default, purely a defensive throttle.
	premiumizeRateLimit = 300
	pmPollInterval      = 1 * time.Second
	pmPollTimeout       = 20 * time.Minute
)

// PremiumizeClient implements Provider against the Premiumize API
// (https://www.premiumize.me/api): first tries transfer/directdl, which
// resolves a magnet straight to direct links without ever landing in the
// account's cloud storage (works whenever the content is already in
// Premiumize's global cache, the common case right after a search). If
// that comes back empty, falls back to the slower transfer/create -> poll
// -> folder/list (or item/details for a single-file result) flow.
type PremiumizeClient struct {
	httpClient   *http.Client
	baseURL      string
	limiter      *Limiter
	pollInterval time.Duration
}

func NewPremiumizeClient() *PremiumizeClient {
	return &PremiumizeClient{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      premiumizeBaseURL,
		limiter:      NewLimiter(premiumizeRateLimit),
		pollInterval: pmPollInterval,
	}
}

func (c *PremiumizeClient) Name() string { return "premiumize" }

func (c *PremiumizeClient) Resolve(ctx context.Context, apiKey, magnetOrHash string) (*ResolveResult, error) {
	magnet := asMagnet(magnetOrHash)

	// directDL never creates a transfer at all -- there's nothing to Delete
	// later, so ProviderRef is deliberately left empty in this path.
	if files, err := c.directDL(ctx, apiKey, magnet); err == nil && len(files) > 0 {
		return &ResolveResult{Files: files}, nil
	}

	transferID, err := c.createTransfer(ctx, apiKey, magnet)
	if err != nil {
		return nil, fmt.Errorf("premiumize: creating transfer: %w", err)
	}
	folderID, fileID, err := c.waitForTransfer(ctx, apiKey, transferID)
	if err != nil {
		return nil, err
	}

	var files []ResolvedFile
	if fileID != "" {
		f, err := c.itemDetails(ctx, apiKey, fileID)
		if err != nil {
			return nil, fmt.Errorf("premiumize: fetching file details: %w", err)
		}
		files = []ResolvedFile{*f}
	} else {
		files, err = c.folderFiles(ctx, apiKey, folderID, "")
		if err != nil {
			return nil, fmt.Errorf("premiumize: listing folder: %w", err)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("premiumize: transfer produced no files")
	}
	return &ResolveResult{ProviderRef: transferID, Files: files}, nil
}

// Delete removes a transfer (and its cloud folder) via /transfer/delete,
// reclaiming the active-download slot it held. A no-op for the directDL
// path, which never creates a transfer (providerRef is empty).
func (c *PremiumizeClient) Delete(ctx context.Context, apiKey, providerRef string) error {
	if providerRef == "" {
		return nil
	}
	var resp pmStatus
	if err := c.do(ctx, http.MethodPost, "/transfer/delete", apiKey, url.Values{"id": {providerRef}}, &resp); err != nil {
		return err
	}
	return resp.check()
}

// pmStatus is embedded in every Premiumize response -- "success" or
// "error", with an error message/code only present on failure.
type pmStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// check classifies known Premiumize failure messages onto the shared
// taxonomy -- covers what Vorn actually needs to branch on (stop
// immediately vs. keep going) without hand-enumerating every message
// Premiumize can return.
func (s pmStatus) check() error {
	if s.Status == "success" {
		return nil
	}
	msg := s.Message
	if msg == "" {
		msg = fmt.Sprintf("request failed with status %q", s.Status)
	}
	err := fmt.Errorf("premiumize: %s", msg)
	switch {
	case strings.Contains(msg, "Not logged in"):
		return &ClassifiedError{Kind: FailureAuth, Err: err}
	case strings.Contains(msg, "not premium"), strings.Contains(msg, "Fair use limit"), strings.Contains(msg, "in progress"):
		return &ClassifiedError{Kind: FailureQuotaExceeded, Err: err}
	default:
		return err
	}
}

type pmDirectDLResponse struct {
	pmStatus
	Content []struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
		Link string `json:"link"`
	} `json:"content"`
}

func (c *PremiumizeClient) directDL(ctx context.Context, apiKey, src string) ([]ResolvedFile, error) {
	var resp pmDirectDLResponse
	if err := c.do(ctx, http.MethodPost, "/transfer/directdl", apiKey, url.Values{"src": {src}}, &resp); err != nil {
		return nil, err
	}
	if err := resp.check(); err != nil {
		return nil, err
	}
	out := make([]ResolvedFile, 0, len(resp.Content))
	for _, f := range resp.Content {
		if f.Link == "" {
			continue
		}
		out = append(out, ResolvedFile{Name: f.Path, SizeBytes: f.Size, StreamURL: f.Link})
	}
	return out, nil
}

type pmCreateTransferResponse struct {
	pmStatus
	ID string `json:"id"`
}

func (c *PremiumizeClient) createTransfer(ctx context.Context, apiKey, src string) (string, error) {
	var resp pmCreateTransferResponse
	if err := c.do(ctx, http.MethodPost, "/transfer/create", apiKey, url.Values{"src": {src}}, &resp); err != nil {
		return "", err
	}
	if err := resp.check(); err != nil {
		return "", err
	}
	return resp.ID, nil
}

type pmTransferListResponse struct {
	pmStatus
	Transfers []struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Message  string `json:"message"`
		FolderID string `json:"folder_id"`
		FileID   string `json:"file_id"`
	} `json:"transfers"`
}

// pmTerminalErrorStatuses are Premiumize transfer statuses that will never
// progress further -- "banned" (content flagged/removed) and "timeout"
// (download stalled out) are just as permanent as "error"/"deleted", so
// treating only the latter two as terminal meant Vorn burned the full
// 20-minute pmPollTimeout on a transfer Premiumize had already given up on.
var pmTerminalErrorStatuses = map[string]bool{
	"error":   true,
	"deleted": true,
	"banned":  true,
	"timeout": true,
}

func (c *PremiumizeClient) waitForTransfer(ctx context.Context, apiKey, transferID string) (folderID, fileID string, err error) {
	deadline := time.Now().Add(pmPollTimeout)
	for {
		var resp pmTransferListResponse
		reqErr := c.do(ctx, http.MethodGet, "/transfer/list", apiKey, nil, &resp)
		if reqErr == nil {
			reqErr = resp.check()
		}
		if reqErr != nil {
			var ce *ClassifiedError
			if errors.As(reqErr, &ce) && ce.Kind == FailureRateLimited {
				if wait := capRetry(ce.RetryAfter, deadline); wait > 0 {
					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						return "", "", ctx.Err()
					}
				}
			}
			return "", "", reqErr
		}
		for _, t := range resp.Transfers {
			if t.ID != transferID {
				continue
			}
			if pmTerminalErrorStatuses[t.Status] {
				return "", "", &ClassifiedError{Kind: FailurePermanent, Err: fmt.Errorf("premiumize: transfer %s: %s", transferID, t.Message)}
			}
			if t.Status == "finished" || t.Status == "seeding" {
				return t.FolderID, t.FileID, nil
			}
			break
		}
		if time.Now().After(deadline) {
			return "", "", fmt.Errorf("premiumize: transfer %s: timed out waiting for it to finish", transferID)
		}
		select {
		case <-time.After(c.pollInterval):
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}
}

type pmFolderListResponse struct {
	pmStatus
	Content []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Type       string `json:"type"`
		Size       int64  `json:"size"`
		Link       string `json:"link"`
		StreamLink string `json:"stream_link"`
	} `json:"content"`
}

// folderFiles lists folderID's contents, recursing into any nested
// subfolders (relPrefix accumulates the path so far) -- Premiumize commonly
// organizes multi-file torrents (season packs, extras) into subfolders, and
// a non-recursive listing would silently drop everything under them.
func (c *PremiumizeClient) folderFiles(ctx context.Context, apiKey, folderID, relPrefix string) ([]ResolvedFile, error) {
	q := url.Values{}
	if folderID != "" {
		q.Set("id", folderID)
	}
	var resp pmFolderListResponse
	if err := c.do(ctx, http.MethodGet, "/folder/list?"+q.Encode(), apiKey, nil, &resp); err != nil {
		return nil, err
	}
	if err := resp.check(); err != nil {
		return nil, err
	}
	var out []ResolvedFile
	for _, f := range resp.Content {
		if f.Type == "folder" {
			nested, err := c.folderFiles(ctx, apiKey, f.ID, path.Join(relPrefix, f.Name))
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		link := f.StreamLink
		if link == "" {
			link = f.Link
		}
		if f.Type != "file" || link == "" {
			continue
		}
		out = append(out, ResolvedFile{Name: path.Join(relPrefix, f.Name), SizeBytes: f.Size, StreamURL: link})
	}
	return out, nil
}

type pmItemDetailsResponse struct {
	pmStatus
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Link       string `json:"link"`
	StreamLink string `json:"stream_link"`
}

func (c *PremiumizeClient) itemDetails(ctx context.Context, apiKey, fileID string) (*ResolvedFile, error) {
	var resp pmItemDetailsResponse
	if err := c.do(ctx, http.MethodGet, "/item/details?id="+url.QueryEscape(fileID), apiKey, nil, &resp); err != nil {
		return nil, err
	}
	if err := resp.check(); err != nil {
		return nil, err
	}
	link := resp.StreamLink
	if link == "" {
		link = resp.Link
	}
	return &ResolvedFile{Name: resp.Name, SizeBytes: resp.Size, StreamURL: link}, nil
}

type pmAccountInfoResponse struct {
	pmStatus
	CustomerID   int64  `json:"customer_id"`
	PremiumUntil *int64 `json:"premium_until"`
}

// AccountInfo calls Premiumize's GET /account/info, the documented way to
// check an API key's validity and premium status.
func (c *PremiumizeClient) AccountInfo(ctx context.Context, apiKey string) (*AccountInfo, error) {
	var resp pmAccountInfoResponse
	if err := c.do(ctx, http.MethodGet, "/account/info", apiKey, nil, &resp); err != nil {
		return nil, fmt.Errorf("premiumize: fetching account info: %w", err)
	}
	if err := resp.check(); err != nil {
		return nil, err
	}
	info := &AccountInfo{
		Username: fmt.Sprintf("customer #%d", resp.CustomerID),
		Premium:  resp.PremiumUntil != nil,
	}
	if info.Premium {
		info.Detail = "premium until " + time.Unix(*resp.PremiumUntil, 0).Format("2006-01-02")
	} else {
		info.Detail = "free account (no premium)"
	}
	return info, nil
}

// do issues a request with the API key as a Bearer token -- Premiumize
// documents this as the recommended auth method over the legacy apikey
// query parameter. form is nil for GETs (params go in the path/query).
func (c *PremiumizeClient) do(ctx context.Context, method, path, apiKey string, form url.Values, out any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}

	var body io.Reader
	contentType := ""
	if form != nil {
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return &ClassifiedError{Kind: FailureRateLimited, RetryAfter: retryAfter(resp.Header), Err: fmt.Errorf("premiumize: rate limited (429) on %s %s", method, path)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("premiumize: %s %s: unexpected status %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

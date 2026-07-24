package debrid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	debridLinkBaseURL = "https://debrid-link.com/api/v2"
	// No documented per-key rate cap found -- conservative default, purely
	// a defensive throttle.
	debridLinkRateLimit = 300
	dlPollInterval      = 3 * time.Second
	dlPollTimeout       = 20 * time.Minute
)

// DebridLinkClient implements Provider against the Debrid-Link API v2
// (https://debrid-link.com/api_doc): add a torrent to the account's
// seedbox, poll until every file is fully downloaded, then return each
// file's already-direct downloadUrl. A static "Private API key" (generated
// in the account's webapp) authenticates identically to an OAuth access
// token -- both go in the Authorization: Bearer header.
type DebridLinkClient struct {
	httpClient   *http.Client
	baseURL      string
	limiter      *limiter
	pollInterval time.Duration
}

func NewDebridLinkClient() *DebridLinkClient {
	return &DebridLinkClient{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      debridLinkBaseURL,
		limiter:      newLimiter(debridLinkRateLimit),
		pollInterval: dlPollInterval,
	}
}

func (c *DebridLinkClient) Name() string { return "debridlink" }

func (c *DebridLinkClient) Resolve(ctx context.Context, apiKey, magnetOrHash string) ([]ResolvedFile, error) {
	magnet := asMagnet(magnetOrHash)

	id, err := c.addTorrent(ctx, apiKey, magnet)
	if err != nil {
		return nil, fmt.Errorf("debridlink: adding torrent: %w", err)
	}
	files, err := c.waitUntilReady(ctx, apiKey, id)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("debridlink: torrent %s has no files", id)
	}
	return files, nil
}

// dlEnvelope is Debrid-Link's response shape: {"success":bool,"value":...}
// on success, {"success":false,"error":"errorCode"} on failure -- no
// "status" field the way AllDebrid/Premiumize use, confirmed against the
// API's own live self-documentation endpoint (/api_doc/infos).
type dlEnvelope[T any] struct {
	Success bool   `json:"success"`
	Value   T      `json:"value"`
	Error   string `json:"error,omitempty"`
}

func (e dlEnvelope[T]) check() error {
	if e.Success {
		return nil
	}
	if e.Error != "" {
		return fmt.Errorf("debridlink: %s", e.Error)
	}
	return fmt.Errorf("debridlink: request failed")
}

type dlFile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Size            int64  `json:"size"`
	DownloadURL     string `json:"downloadUrl"`
	DownloadPercent int    `json:"downloadPercent"`
}

// dlTorrent's Status is documented as: 0 paused, 1 queued, 2 verification,
// 4 downloading, 8 seeding, 100 finished -- but a seeding (8) torrent can
// already have every file at DownloadPercent 100, so readiness is checked
// via the aggregate DownloadPercent field, not Status, per the API docs'
// own guidance ("a file is ready for download when files[].downloadPercent
// == 100").
type dlTorrent struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Status          int      `json:"status"`
	DownloadPercent int      `json:"downloadPercent"`
	Files           []dlFile `json:"files"`
}

func (c *DebridLinkClient) addTorrent(ctx context.Context, apiKey, magnet string) (string, error) {
	var resp dlEnvelope[dlTorrent]
	if err := c.do(ctx, http.MethodPost, "/seedbox/add", apiKey, url.Values{"url": {magnet}}, &resp); err != nil {
		return "", err
	}
	if err := resp.check(); err != nil {
		return "", err
	}
	return resp.Value.ID, nil
}

func (c *DebridLinkClient) waitUntilReady(ctx context.Context, apiKey, id string) ([]ResolvedFile, error) {
	deadline := time.Now().Add(dlPollTimeout)
	for {
		var resp dlEnvelope[[]dlTorrent]
		if err := c.do(ctx, http.MethodGet, "/seedbox/list?ids="+url.QueryEscape(id), apiKey, nil, &resp); err != nil {
			return nil, err
		}
		if err := resp.check(); err != nil {
			return nil, err
		}
		if len(resp.Value) == 0 {
			return nil, fmt.Errorf("debridlink: torrent %s not found while polling status", id)
		}
		t := resp.Value[0]
		if t.DownloadPercent >= 100 {
			out := make([]ResolvedFile, 0, len(t.Files))
			for _, f := range t.Files {
				if f.DownloadURL == "" {
					continue
				}
				out = append(out, ResolvedFile{Name: f.Name, SizeBytes: f.Size, StreamURL: f.DownloadURL})
			}
			return out, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("debridlink: torrent %s: timed out waiting to finish downloading", id)
		}
		select {
		case <-time.After(c.pollInterval):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type dlAccountInfo struct {
	Username    string `json:"username"`
	AccountType int    `json:"accountType"`
	PremiumLeft int64  `json:"premiumLeft"` // seconds of premium remaining, 0 if free
}

// AccountInfo calls Debrid-Link's GET /account/infos, the documented way
// to check an API key's validity and premium status.
func (c *DebridLinkClient) AccountInfo(ctx context.Context, apiKey string) (*AccountInfo, error) {
	var resp dlEnvelope[dlAccountInfo]
	if err := c.do(ctx, http.MethodGet, "/account/infos", apiKey, nil, &resp); err != nil {
		return nil, fmt.Errorf("debridlink: fetching account info: %w", err)
	}
	if err := resp.check(); err != nil {
		return nil, err
	}
	info := &AccountInfo{Username: resp.Value.Username, Premium: resp.Value.PremiumLeft > 0}
	if info.Premium {
		info.Detail = fmt.Sprintf("premium, %s left", time.Duration(resp.Value.PremiumLeft)*time.Second)
	} else {
		info.Detail = "free account (no premium)"
	}
	return info, nil
}

// do issues a request with the API key as a Bearer token. form is nil for
// GETs (params go in the path/query, as built by callers above).
func (c *DebridLinkClient) do(ctx context.Context, method, path, apiKey string, form url.Values, out any) error {
	if err := c.limiter.wait(ctx); err != nil {
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
		return fmt.Errorf("debridlink: rate limited (429) on %s %s", method, path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("debridlink: %s %s: unexpected status %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

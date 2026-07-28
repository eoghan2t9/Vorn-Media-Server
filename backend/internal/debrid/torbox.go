package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	torBoxBaseURL   = "https://api.torbox.app/v1/api"
	torBoxRateLimit = 300 // requests/minute, per TorBox's API docs
	tbPollInterval  = 3 * time.Second
	tbPollTimeout   = 20 * time.Minute
)

// TorBoxClient implements Provider against the TorBox API
// (https://api-docs.torbox.app/): create a torrent from a magnet link, wait
// for it to be cached, then request a metered, time-limited direct download
// link per file.
type TorBoxClient struct {
	httpClient   *http.Client
	baseURL      string
	limiter      *Limiter
	pollInterval time.Duration
}

// NewTorBoxClient takes limiter rather than constructing its own, since
// TorBox's account credentials/rate limit apply account-wide -- Vorn talks
// to TorBox from three independent services (this debrid-resolve client,
// nzb.Service's usenet caching, torrent.Service's indexer search), and only
// a single shared Limiter instance (see debrid.Service.TorBoxLimiter) makes
// the 300/min cap a real, enforced budget across all three rather than
// each getting its own independent window.
func NewTorBoxClient(limiter *Limiter) *TorBoxClient {
	return &TorBoxClient{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      torBoxBaseURL,
		limiter:      limiter,
		pollInterval: tbPollInterval,
	}
}

func (c *TorBoxClient) Name() string { return "torbox" }

func (c *TorBoxClient) Resolve(ctx context.Context, apiKey, magnetOrHash string) (*ResolveResult, error) {
	magnet := asMagnet(magnetOrHash)

	torrentID, err := c.createTorrent(ctx, apiKey, magnet)
	if err != nil {
		return nil, fmt.Errorf("torbox: creating torrent: %w", err)
	}

	files, err := c.waitForCache(ctx, apiKey, torrentID)
	if err != nil {
		return nil, err
	}

	out := make([]ResolvedFile, 0, len(files))
	for _, f := range files {
		link, err := c.requestDownloadLink(ctx, apiKey, torrentID, f.ID)
		if err != nil {
			return nil, fmt.Errorf("torbox: requesting download link for file %d: %w", f.ID, err)
		}
		name := f.Name
		if name == "" {
			name = f.ShortName
		}
		out = append(out, ResolvedFile{Name: name, SizeBytes: f.Size, StreamURL: link})
	}
	return &ResolveResult{ProviderRef: strconv.Itoa(torrentID), Files: out}, nil
}

// Delete removes a torrent from the account via POST /torrents/controltorrent
// (operation=delete), reclaiming the active-torrent slot it held.
func (c *TorBoxClient) Delete(ctx context.Context, apiKey, providerRef string) error {
	if providerRef == "" {
		return nil
	}
	return c.controlDownload(ctx, apiKey, "/torrents/controltorrent", "torrent_id", providerRef)
}

// DeleteUsenetDownload mirrors Delete but against the Usenet endpoint (POST
// /usenet/controlusenetdownload, operation=delete) -- TorBox's documented
// equivalent for a download cached from an NZB rather than a torrent. Used
// by nzb.Service.Remove to reclaim TorBox usenet storage/quota.
func (c *TorBoxClient) DeleteUsenetDownload(ctx context.Context, apiKey string, usenetID int) error {
	if usenetID == 0 {
		return nil
	}
	return c.controlDownload(ctx, apiKey, "/usenet/controlusenetdownload", "usenet_id", strconv.Itoa(usenetID))
}

func (c *TorBoxClient) controlDownload(ctx context.Context, apiKey, path, idField, id string) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField(idField, id); err != nil {
		return err
	}
	if err := w.WriteField("operation", "delete"); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	var resp tbEnvelope[json.RawMessage]
	if err := c.do(ctx, http.MethodPost, path, apiKey, w.FormDataContentType(), &body, &resp); err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("torbox: %s", resp.Detail)
	}
	return nil
}

func (c *TorBoxClient) waitForCache(ctx context.Context, apiKey string, torrentID int) ([]tbFile, error) {
	deadline := time.Now().Add(tbPollTimeout)
	for {
		item, err := c.torrentInfo(ctx, apiKey, torrentID)
		if err != nil {
			var transient *tbTransientError
			var ce *ClassifiedError
			switch {
			case errors.As(err, &transient):
				log.Printf("torbox: transient error polling torrent %d, retrying: %v", torrentID, err)
			case errors.As(err, &ce) && ce.Kind == FailureRateLimited:
				if wait := capRetry(ce.RetryAfter, deadline); wait > 0 {
					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				return nil, err
			default:
				return nil, err
			}
		}
		if item != nil && item.DownloadFinished {
			return item.Files, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("torbox: torrent %d: timed out waiting for caching to finish", torrentID)
		}
		select {
		case <-time.After(c.pollInterval):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type tbEnvelope[T any] struct {
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
	Data    T      `json:"data"`
}

type tbCreateTorrentData struct {
	TorrentID float64 `json:"torrent_id"`
	Hash      string  `json:"hash"`
}

func (c *TorBoxClient) createTorrent(ctx context.Context, apiKey, magnet string) (int, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("magnet", magnet); err != nil {
		return 0, err
	}
	if err := w.Close(); err != nil {
		return 0, err
	}

	var resp tbEnvelope[tbCreateTorrentData]
	if err := c.do(ctx, http.MethodPost, "/torrents/createtorrent", apiKey, w.FormDataContentType(), &body, &resp); err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("torbox: %s", resp.Detail)
	}
	return int(resp.Data.TorrentID), nil
}

type tbFile struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Size      int64  `json:"size"`
}

type tbTorrentInfo struct {
	ID               int      `json:"id"`
	DownloadFinished bool     `json:"download_finished"`
	Files            []tbFile `json:"files"`
}

func (c *TorBoxClient) torrentInfo(ctx context.Context, apiKey string, torrentID int) (*tbTorrentInfo, error) {
	path := "/torrents/mylist?bypass_cache=true&id=" + url.QueryEscape(strconv.Itoa(torrentID))
	var resp tbEnvelope[[]tbTorrentInfo]
	if err := c.do(ctx, http.MethodGet, path, apiKey, "", nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("torbox: %s", resp.Detail)
	}
	for _, item := range resp.Data {
		if item.ID == torrentID {
			return &item, nil
		}
	}
	if len(resp.Data) > 0 {
		return &resp.Data[0], nil
	}
	return nil, nil
}

// requestDownloadLink authenticates via the token query parameter, matching
// TorBox's documented auth method for this specific endpoint (every other
// endpoint uses the Authorization header).
func (c *TorBoxClient) requestDownloadLink(ctx context.Context, apiKey string, torrentID, fileID int) (string, error) {
	path := fmt.Sprintf("/torrents/requestdl?token=%s&torrent_id=%d&file_id=%d",
		url.QueryEscape(apiKey), torrentID, fileID)
	var resp tbEnvelope[string]
	if err := c.do(ctx, http.MethodGet, path, "", "", nil, &resp); err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("torbox: %s", resp.Detail)
	}
	return resp.Data, nil
}

// tbCreateUsenetData: the new download's ID comes back as usenetdownload_id
// (not usenet_id, which was the original bug here -- it silently
// zero-valued, so every subsequent poll/download-link call operated on
// id=0, which usenetInfo's "fall back to the first list entry" branch
// masked by silently returning whatever unrelated download happened to be
// first in the account's list). It's a JSON number, confirmed directly
// against the live API (a community SDK had modeled it as a string, which
// production immediately rejected with a JSON unmarshal error).
type tbCreateUsenetData struct {
	UsenetDownloadID float64 `json:"usenetdownload_id"`
	Hash             string  `json:"hash"`
}

// CreateUsenetDownload submits a raw .nzb file to TorBox's own Usenet
// backend (POST /usenet/createusenetdownload): TorBox downloads, yEnc
// decodes, and par2-repairs it server-side, entirely off-box.
func (c *TorBoxClient) CreateUsenetDownload(ctx context.Context, apiKey string, nzbData []byte, name string) (int, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name+".nzb")
	if err != nil {
		return 0, err
	}
	if _, err := part.Write(nzbData); err != nil {
		return 0, err
	}
	if err := w.WriteField("name", name); err != nil {
		return 0, err
	}
	if err := w.Close(); err != nil {
		return 0, err
	}

	var resp tbEnvelope[tbCreateUsenetData]
	if err := c.do(ctx, http.MethodPost, "/usenet/createusenetdownload", apiKey, w.FormDataContentType(), &body, &resp); err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("torbox: %s", resp.Detail)
	}
	return int(resp.Data.UsenetDownloadID), nil
}

type tbUsenetInfo struct {
	ID               int  `json:"id"`
	DownloadFinished bool `json:"download_finished"`
	// DownloadPresent lags DownloadFinished: TorBox's own SDK models these
	// as separate booleans (unlike the torrent side, where the file list is
	// known upfront from the torrent's metadata) -- download_finished flips
	// once the repair job itself is done, but the file listing is only
	// populated once TorBox's server-side extraction/finalization catches
	// up, signaled by download_present. Treating DownloadFinished alone as
	// "ready" was observed in production returning a real, finished item
	// with an empty Files slice.
	DownloadPresent bool    `json:"download_present"`
	Progress        float64 `json:"progress"`
	// DownloadState carries human-readable states like "downloading",
	// "completed", "failed", or "cached" -- checked below so a genuinely
	// broken NZB (missing articles, etc) fails fast instead of burning the
	// full tbPollTimeout waiting for download_finished/download_present to
	// flip, which they never will.
	DownloadState string   `json:"download_state"`
	Files         []tbFile `json:"files"`
}

func (i *tbUsenetInfo) failed() bool {
	s := strings.ToLower(i.DownloadState)
	return strings.Contains(s, "failed") || strings.Contains(s, "invalid") || strings.Contains(s, "error")
}

// WaitForUsenetCache polls GET /usenet/mylist until TorBox finishes
// downloading, repairing, and finalizing usenetID's file listing,
// invoking progress (0..1) as it goes so callers can mirror it into their
// own byte-progress tracking.
func (c *TorBoxClient) WaitForUsenetCache(ctx context.Context, apiKey string, usenetID int, progress func(float64)) ([]tbFile, error) {
	deadline := time.Now().Add(tbPollTimeout)
	for {
		item, err := c.usenetInfo(ctx, apiKey, usenetID)
		if err != nil {
			var transient *tbTransientError
			var ce *ClassifiedError
			switch {
			case errors.As(err, &transient):
				log.Printf("torbox: transient error polling usenet %d, retrying: %v", usenetID, err)
			case errors.As(err, &ce) && ce.Kind == FailureRateLimited:
				if wait := capRetry(ce.RetryAfter, deadline); wait > 0 {
					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				return nil, err
			default:
				return nil, err
			}
		}
		if item != nil {
			if item.failed() {
				return nil, &ClassifiedError{Kind: FailurePermanent, Err: fmt.Errorf("torbox: usenet download %d: %s", usenetID, item.DownloadState)}
			}
			if progress != nil {
				progress(item.Progress)
			}
			if item.DownloadFinished && item.DownloadPresent {
				return item.Files, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("torbox: usenet download %d: timed out waiting for caching to finish", usenetID)
		}
		select {
		case <-time.After(c.pollInterval):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// usenetInfo fetches a single usenet download's status. Unlike
// /torrents/mylist (which always wraps its result in a JSON array, even
// when filtered down to one id), /usenet/mylist?id=X was observed in
// production returning data as a bare JSON object rather than a
// single-element array -- so data is decoded as json.RawMessage and
// unmarshaled as whichever shape it actually is, rather than assuming one.
func (c *TorBoxClient) usenetInfo(ctx context.Context, apiKey string, usenetID int) (*tbUsenetInfo, error) {
	path := "/usenet/mylist?bypass_cache=true&id=" + url.QueryEscape(strconv.Itoa(usenetID))
	var resp tbEnvelope[json.RawMessage]
	if err := c.do(ctx, http.MethodGet, path, apiKey, "", nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("torbox: %s", resp.Detail)
	}
	raw := bytes.TrimSpace(resp.Data)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	if raw[0] == '[' {
		var items []tbUsenetInfo
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("torbox: decoding usenet list: %w", err)
		}
		for _, item := range items {
			if item.ID == usenetID {
				return &item, nil
			}
		}
		if len(items) > 0 {
			return &items[0], nil
		}
		return nil, nil
	}

	var item tbUsenetInfo
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("torbox: decoding usenet item: %w", err)
	}
	return &item, nil
}

// RequestUsenetDownloadLink mirrors requestDownloadLink but against the
// Usenet endpoint (GET /usenet/requestdl), TorBox's documented equivalent
// for files cached from an NZB rather than a torrent.
func (c *TorBoxClient) RequestUsenetDownloadLink(ctx context.Context, apiKey string, usenetID, fileID int) (string, error) {
	path := fmt.Sprintf("/usenet/requestdl?token=%s&usenet_id=%d&file_id=%d",
		url.QueryEscape(apiKey), usenetID, fileID)
	var resp tbEnvelope[string]
	if err := c.do(ctx, http.MethodGet, path, "", "", nil, &resp); err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("torbox: %s", resp.Detail)
	}
	return resp.Data, nil
}

type tbUserData struct {
	Email            string  `json:"email"`
	Plan             float64 `json:"plan"`
	IsSubscribed     bool    `json:"is_subscribed"`
	PremiumExpiresAt string  `json:"premium_expires_at"`
}

// AccountInfo calls TorBox's GET /user/me, confirmed against the official
// Go SDK (torbox-sdk-go/pkg/user) for the exact response field names --
// requires only the Authorization header, no write access.
func (c *TorBoxClient) AccountInfo(ctx context.Context, apiKey string) (*AccountInfo, error) {
	var resp tbEnvelope[tbUserData]
	if err := c.do(ctx, http.MethodGet, "/user/me", apiKey, "", nil, &resp); err != nil {
		return nil, fmt.Errorf("torbox: fetching account info: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("torbox: %s", resp.Detail)
	}
	info := &AccountInfo{Username: resp.Data.Email, Premium: resp.Data.IsSubscribed}
	if info.Premium {
		info.Detail = "subscribed, expires " + resp.Data.PremiumExpiresAt
	} else {
		info.Detail = "free account (not subscribed)"
	}
	return info, nil
}

func (c *TorBoxClient) do(ctx context.Context, method, path, apiKey, contentType string, body io.Reader, out any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return &ClassifiedError{Kind: FailureRateLimited, RetryAfter: retryAfter(resp.Header), Err: fmt.Errorf("torbox: rate limited (429) on %s %s", method, path)}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return &ClassifiedError{Kind: FailureAuth, Err: fmt.Errorf("torbox: unauthorized (401) on %s %s", method, path)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := fmt.Errorf("torbox: %s %s: unexpected status %d: %s", method, path, resp.StatusCode, string(data))
		if resp.StatusCode >= 500 {
			return &tbTransientError{err: err}
		}
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// tbTransientError marks a TorBox API response as a transient server-side
// failure (5xx) -- observed in production as an intermittent
// "DATABASE_ERROR ... please try again later" on /usenet/mylist against an
// otherwise perfectly healthy, in-progress download. waitForCache and
// WaitForUsenetCache treat this as "try again next poll" rather than
// aborting the whole download over a single hiccup mid-poll.
type tbTransientError struct{ err error }

func (e *tbTransientError) Error() string { return e.err.Error() }
func (e *tbTransientError) Unwrap() error { return e.err }

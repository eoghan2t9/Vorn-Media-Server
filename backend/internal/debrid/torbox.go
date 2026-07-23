package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
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
	limiter      *limiter
	pollInterval time.Duration
}

func NewTorBoxClient() *TorBoxClient {
	return &TorBoxClient{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      torBoxBaseURL,
		limiter:      newLimiter(torBoxRateLimit),
		pollInterval: tbPollInterval,
	}
}

func (c *TorBoxClient) Name() string { return "torbox" }

func (c *TorBoxClient) Resolve(ctx context.Context, apiKey, magnetOrHash string) ([]ResolvedFile, error) {
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
	return out, nil
}

func (c *TorBoxClient) waitForCache(ctx context.Context, apiKey string, torrentID int) ([]tbFile, error) {
	deadline := time.Now().Add(tbPollTimeout)
	for {
		item, err := c.torrentInfo(ctx, apiKey, torrentID)
		if err != nil {
			return nil, err
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

func (c *TorBoxClient) do(ctx context.Context, method, path, apiKey, contentType string, body io.Reader, out any) error {
	if err := c.limiter.wait(ctx); err != nil {
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
		return fmt.Errorf("torbox: rate limited (429) on %s %s", method, path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("torbox: %s %s: unexpected status %d: %s", method, path, resp.StatusCode, string(data))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

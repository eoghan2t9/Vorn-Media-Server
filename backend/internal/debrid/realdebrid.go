package debrid

import (
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
	realDebridBaseURL   = "https://api.real-debrid.com/rest/1.0"
	realDebridRateLimit = 250 // requests/minute, per Real-Debrid's API docs
	rdPollInterval      = 3 * time.Second
	rdPollTimeout       = 20 * time.Minute
)

// RealDebridClient implements Provider against the Real-Debrid REST API
// (https://api.real-debrid.com/): add a magnet, wait for it to be cached and
// its files selected, then unrestrict each resulting hoster link into a
// direct download URL.
type RealDebridClient struct {
	httpClient   *http.Client
	baseURL      string
	limiter      *limiter
	pollInterval time.Duration
}

func NewRealDebridClient() *RealDebridClient {
	return &RealDebridClient{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      realDebridBaseURL,
		limiter:      newLimiter(realDebridRateLimit),
		pollInterval: rdPollInterval,
	}
}

func (c *RealDebridClient) Name() string { return "realdebrid" }

func (c *RealDebridClient) Resolve(ctx context.Context, apiKey, magnetOrHash string) ([]ResolvedFile, error) {
	magnet := asMagnet(magnetOrHash)

	id, err := c.addMagnet(ctx, apiKey, magnet)
	if err != nil {
		return nil, fmt.Errorf("realdebrid: adding magnet: %w", err)
	}

	info, err := c.pollUntil(ctx, apiKey, id, func(i *rdTorrentInfo) bool {
		return i.Status == "waiting_files_selection"
	})
	if err != nil {
		return nil, err
	}

	fileIDs := make([]string, 0, len(info.Files))
	for _, f := range info.Files {
		fileIDs = append(fileIDs, strconv.Itoa(f.ID))
	}
	if len(fileIDs) == 0 {
		return nil, fmt.Errorf("realdebrid: torrent %s has no files", id)
	}
	if err := c.selectFiles(ctx, apiKey, id, strings.Join(fileIDs, ",")); err != nil {
		return nil, fmt.Errorf("realdebrid: selecting files: %w", err)
	}

	info, err = c.pollUntil(ctx, apiKey, id, func(i *rdTorrentInfo) bool {
		return i.Status == "downloaded"
	})
	if err != nil {
		return nil, err
	}

	out := make([]ResolvedFile, 0, len(info.Links))
	for _, link := range info.Links {
		unrestricted, err := c.unrestrictLink(ctx, apiKey, link)
		if err != nil {
			return nil, fmt.Errorf("realdebrid: unrestricting link: %w", err)
		}
		out = append(out, ResolvedFile{
			Name:      unrestricted.Filename,
			SizeBytes: unrestricted.Filesize,
			StreamURL: unrestricted.Download,
		})
	}
	return out, nil
}

// rdTerminalStatuses are Real-Debrid torrent statuses that will never
// progress further; polling should stop and report an error immediately
// rather than waiting out the full timeout.
var rdTerminalStatuses = map[string]bool{
	"magnet_error": true,
	"error":        true,
	"virus":        true,
	"dead":         true,
}

func (c *RealDebridClient) pollUntil(ctx context.Context, apiKey, id string, done func(*rdTorrentInfo) bool) (*rdTorrentInfo, error) {
	deadline := time.Now().Add(rdPollTimeout)
	for {
		info, err := c.torrentInfo(ctx, apiKey, id)
		if err != nil {
			return nil, err
		}
		if rdTerminalStatuses[info.Status] {
			return nil, fmt.Errorf("realdebrid: torrent %s: status %q", id, info.Status)
		}
		if done(info) {
			return info, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("realdebrid: torrent %s: timed out waiting past status %q", id, info.Status)
		}
		select {
		case <-time.After(c.pollInterval):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type rdAddMagnetResponse struct {
	ID string `json:"id"`
}

func (c *RealDebridClient) addMagnet(ctx context.Context, apiKey, magnet string) (string, error) {
	form := url.Values{"magnet": {magnet}}
	var resp rdAddMagnetResponse
	if err := c.doForm(ctx, http.MethodPost, "/torrents/addMagnet", apiKey, form, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

type rdFile struct {
	ID       int    `json:"id"`
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	Selected int    `json:"selected"`
}

type rdTorrentInfo struct {
	Status string   `json:"status"`
	Files  []rdFile `json:"files"`
	Links  []string `json:"links"`
}

func (c *RealDebridClient) torrentInfo(ctx context.Context, apiKey, id string) (*rdTorrentInfo, error) {
	var info rdTorrentInfo
	if err := c.doJSON(ctx, http.MethodGet, "/torrents/info/"+url.PathEscape(id), apiKey, nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *RealDebridClient) selectFiles(ctx context.Context, apiKey, id, fileIDs string) error {
	form := url.Values{"files": {fileIDs}}
	return c.doForm(ctx, http.MethodPost, "/torrents/selectFiles/"+url.PathEscape(id), apiKey, form, nil)
}

type rdUnrestrictedLink struct {
	Filename string `json:"filename"`
	Filesize int64  `json:"filesize"`
	Download string `json:"download"`
}

func (c *RealDebridClient) unrestrictLink(ctx context.Context, apiKey, link string) (*rdUnrestrictedLink, error) {
	form := url.Values{"link": {link}}
	var resp rdUnrestrictedLink
	if err := c.doForm(ctx, http.MethodPost, "/unrestrict/link", apiKey, form, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// doJSON issues a GET (or any body-less request) and decodes a JSON response.
func (c *RealDebridClient) doJSON(ctx context.Context, method, path, apiKey string, body io.Reader, out any) error {
	return c.do(ctx, method, path, apiKey, "", body, out)
}

// doForm issues a request with an application/x-www-form-urlencoded body,
// used for every Real-Debrid write endpoint.
func (c *RealDebridClient) doForm(ctx context.Context, method, path, apiKey string, form url.Values, out any) error {
	return c.do(ctx, method, path, apiKey, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), out)
}

func (c *RealDebridClient) do(ctx context.Context, method, path, apiKey, contentType string, body io.Reader, out any) error {
	if err := c.limiter.wait(ctx); err != nil {
		return err
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
		return fmt.Errorf("realdebrid: rate limited (429) on %s %s", method, path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("realdebrid: %s %s: unexpected status %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func asMagnet(magnetOrHash string) string {
	if strings.HasPrefix(magnetOrHash, "magnet:") {
		return magnetOrHash
	}
	return "magnet:?xt=urn:btih:" + magnetOrHash
}

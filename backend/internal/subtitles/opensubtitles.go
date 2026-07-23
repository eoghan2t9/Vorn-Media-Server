package subtitles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const openSubtitlesBaseURL = "https://api.opensubtitles.com/api/v1"

// Client talks to the OpenSubtitles REST API v1
// (https://opensubtitles.stoplight.io/), field names taken from its
// published docs and the community opensubtitles-com Python wrapper.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	userAgent  string
}

func NewClient(apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    openSubtitlesBaseURL,
		apiKey:     apiKey,
		userAgent:  "VornMediaServer/0.1",
	}
}

type LoginResult struct {
	Token            string
	AllowedDownloads int
}

func (c *Client) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	var resp struct {
		Token string `json:"token"`
		User  struct {
			AllowedDownloads int `json:"allowed_downloads"`
		} `json:"user"`
	}
	body := map[string]string{"username": username, "password": password}
	if err := c.do(ctx, http.MethodPost, "/login", "", body, &resp); err != nil {
		return nil, fmt.Errorf("opensubtitles: login: %w", err)
	}
	return &LoginResult{Token: resp.Token, AllowedDownloads: resp.User.AllowedDownloads}, nil
}

type SearchResult struct {
	FileID        int
	FileName      string
	Release       string
	Language      string
	DownloadCount int
}

// SearchByMovieHash looks up subtitles matched to a file's content hash
// (see ComputeMovieHash), which is far more precise than a filename/title
// search since it doesn't depend on release naming at all.
func (c *Client) SearchByMovieHash(ctx context.Context, token, movieHash, language string) ([]SearchResult, error) {
	path := "/subtitles?" + url.Values{
		"moviehash": {movieHash},
		"languages": {language},
	}.Encode()

	var resp struct {
		Data []struct {
			Attributes struct {
				Release       string `json:"release"`
				Language      string `json:"language"`
				DownloadCount int    `json:"download_count"`
				Files         []struct {
					FileID   int    `json:"file_id"`
					FileName string `json:"file_name"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, token, nil, &resp); err != nil {
		return nil, fmt.Errorf("opensubtitles: searching: %w", err)
	}

	out := make([]SearchResult, 0, len(resp.Data))
	for _, d := range resp.Data {
		if len(d.Attributes.Files) == 0 {
			continue
		}
		out = append(out, SearchResult{
			FileID:        d.Attributes.Files[0].FileID,
			FileName:      d.Attributes.Files[0].FileName,
			Release:       d.Attributes.Release,
			Language:      d.Attributes.Language,
			DownloadCount: d.Attributes.DownloadCount,
		})
	}
	return out, nil
}

type DownloadResult struct {
	Content   []byte
	Remaining int
	ResetTime string
}

// Download resolves a file_id (from SearchByMovieHash) into an actual
// subtitle file's bytes. This is the metered call: each one consumes a unit
// of the account's daily download quota, which is why Service caches the
// result on disk keyed by movie hash + language.
func (c *Client) Download(ctx context.Context, token string, fileID int) (*DownloadResult, error) {
	var resp struct {
		Link      string `json:"link"`
		Remaining int    `json:"remaining"`
		ResetTime string `json:"reset_time"`
		Message   string `json:"message"`
	}
	body := map[string]int{"file_id": fileID}
	if err := c.do(ctx, http.MethodPost, "/download", token, body, &resp); err != nil {
		return nil, fmt.Errorf("opensubtitles: requesting download link: %w", err)
	}
	if resp.Link == "" {
		return nil, fmt.Errorf("opensubtitles: download request failed: %s", resp.Message)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resp.Link, nil)
	if err != nil {
		return nil, err
	}
	dlResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opensubtitles: fetching subtitle file: %w", err)
	}
	defer dlResp.Body.Close()

	content, err := io.ReadAll(io.LimitReader(dlResp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	return &DownloadResult{Content: content, Remaining: resp.Remaining, ResetTime: resp.ResetTime}, nil
}

func (c *Client) do(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: unexpected status %d: %s", method, path, resp.StatusCode, string(data))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

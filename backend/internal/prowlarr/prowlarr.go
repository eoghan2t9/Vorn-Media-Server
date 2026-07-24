// Package prowlarr lets Vorn auto-discover and register the torrent
// indexers a user has configured inside a Prowlarr instance, so bundling
// Prowlarr (see deploy/docker-compose.yml's optional "prowlarr" profile)
// doesn't also require manually copying each indexer's URL/API key into
// Vorn's own Torrent Indexers admin page. Vorn stays a plain Torznab
// client throughout -- this package only talks to Prowlarr's management
// API to discover what's configured, it doesn't do any indexer scraping
// itself.
package prowlarr

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Indexer is the subset of Prowlarr's GET /api/v1/indexer response Vorn
// cares about.
type Indexer struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Enable   bool   `json:"enable"`
	Protocol string `json:"protocol"` // "torrent" | "usenet"
}

// prowlarrConfig models the handful of fields Vorn needs out of Prowlarr's
// own config.xml -- <Config><ApiKey>...</ApiKey></Config>. Sonarr/Radarr/
// Prowlarr all share the same Servarr config-file format, so this same
// shape would work for those too if ever needed.
type prowlarrConfig struct {
	XMLName xml.Name `xml:"Config"`
	APIKey  string   `xml:"ApiKey"`
}

// ReadAPIKey extracts the auto-generated API key from a Prowlarr config.xml
// at path.
func ReadAPIKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var cfg prowlarrConfig
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("prowlarr: parsing %s: %w", path, err)
	}
	if cfg.APIKey == "" {
		return "", fmt.Errorf("prowlarr: %s has no ApiKey yet", path)
	}
	return cfg.APIKey, nil
}

// WaitForAPIKey polls for path to appear and contain a usable API key --
// Prowlarr only writes config.xml after its own first boot, which can take
// a few seconds longer than Vorn's own startup, especially on a fresh
// volume.
func WaitForAPIKey(ctx context.Context, path string, interval, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		key, err := ReadAPIKey(path)
		if err == nil {
			return key, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("prowlarr: waiting for %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
	}
}

// ListIndexers fetches every indexer configured in Prowlarr.
func ListIndexers(ctx context.Context, baseURL, apiKey string) ([]Indexer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v1/indexer", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: contacting %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("prowlarr: %s returned status %d: %s", baseURL, resp.StatusCode, body)
	}

	var indexers []Indexer
	if err := json.NewDecoder(resp.Body).Decode(&indexers); err != nil {
		return nil, fmt.Errorf("prowlarr: decoding indexer list: %w", err)
	}
	return indexers, nil
}

// TorznabBaseURL builds the per-indexer URL Vorn's generic Torznab client
// (internal/torrent) stores and completes by appending "/api" itself --
// the same convention the manual Prowlarr preset on the Torrents admin page
// already uses (frontend/src/pages/AdminTorrents.tsx), confirmed against
// Prowlarr's own NewznabController, which dual-maps
// "/api/v1/indexer/{id}/newznab" and "{id}/api" to the same handler.
func TorznabBaseURL(prowlarrBaseURL string, indexerID int) string {
	return fmt.Sprintf("%s/%d", strings.TrimRight(prowlarrBaseURL, "/"), indexerID)
}

package torrent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// torBoxSearchBaseURL is TorBox's own torrent-search API -- a distinct
// service from api.torbox.app (debrid/usenet), confirmed against TorBox's
// published Prowlarr custom-indexer definition
// (github.com/dreulavelle/Prowlarr-Indexers, Custom/torbox.yml). A var, not
// a const, so tests can point it at an httptest server.
var torBoxSearchBaseURL = "https://search-api.torbox.app"

// torBoxValidationIMDbID (Fight Club) is what TorBox's own reference
// indexer definition uses to validate a key without requiring a real
// search query -- TestTorBoxIndexer mirrors that.
const torBoxValidationIMDbID = "tt0137523"

type torBoxSearchEnvelope struct {
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
	Data    struct {
		Torrents []torBoxSearchTorrent `json:"torrents"`
	} `json:"data"`
}

type torBoxSearchTorrent struct {
	RawTitle         string `json:"raw_title"`
	Magnet           string `json:"magnet"`
	Size             int64  `json:"size"`
	LastKnownSeeders int    `json:"last_known_seeders"`
}

// searchTorBoxIndexer queries TorBox's torrent-search API, which -- unlike
// every Torznab indexer SearchIndexer talks to -- takes no free-text query
// at all: it's strictly GET /torrents/imdb:{id} for a movie, or
// .../torrents/imdb:{id}?season=S&episode=E for a TV episode. There's no
// "whole season" query mode (season+episode are both required), so
// season-pack acquisition sends episode=1 as a representative probe --
// many real season-pack releases are indexed under every episode they
// contain, so this often still surfaces one; scanner.LooksLikeSingleEpisode
// (already used by the NZB season-pack tier) filters the results the same
// way regardless of which tier found them.
func searchTorBoxIndexer(ctx context.Context, name, apiKey, imdbID string, season, episode int) ([]SearchResult, error) {
	path := fmt.Sprintf("/torrents/imdb:%s", imdbID)
	if season > 0 {
		if episode <= 0 {
			episode = 1
		}
		path = fmt.Sprintf("%s?season=%d&episode=%d", path, season, episode)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, torBoxSearchBaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("torrent: querying TorBox indexer %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("torrent: TorBox indexer %s returned status %d: %s", name, resp.StatusCode, string(data))
	}

	var envelope torBoxSearchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("torrent: decoding TorBox indexer %s response: %w", name, err)
	}
	if !envelope.Success {
		return nil, fmt.Errorf("torrent: TorBox indexer %s: %s", name, envelope.Detail)
	}

	out := make([]SearchResult, 0, len(envelope.Data.Torrents))
	for _, t := range envelope.Data.Torrents {
		if t.Magnet == "" {
			continue
		}
		out = append(out, SearchResult{
			IndexerName: name,
			Title:       t.RawTitle,
			SizeBytes:   t.Size,
			Seeders:     t.LastKnownSeeders,
			DownloadURL: t.Magnet,
		})
	}
	return out, nil
}

// TestTorBoxIndexer validates apiKey by running the same validation query
// TorBox's own reference indexer definition uses (a known-good IMDb ID,
// not a real search) -- mirrors TestIndexer's role for Torznab indexers.
func TestTorBoxIndexer(ctx context.Context, apiKey string) error {
	_, err := searchTorBoxIndexer(ctx, "TorBox", apiKey, torBoxValidationIMDbID, 0, 0)
	return err
}

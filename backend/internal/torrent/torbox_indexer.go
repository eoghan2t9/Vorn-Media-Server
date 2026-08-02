package torrent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// torBoxSearchBaseURL is TorBox's own torrent-search API -- a distinct
// service from api.torbox.app (debrid/usenet), confirmed against TorBox's
// published Prowlarr custom-indexer definition
// (github.com/dreulavelle/Prowlarr-Indexers, Custom/torbox.yml).
//
// NOTE: As of Aug 2026, search-api.torbox.app does not resolve (NXDOMAIN
// even via 8.8.8.8). The main api.torbox.app domain also returns 404 for
// the /torrents/imdb_id: path. TorBox may have decommissioned their search
// API or moved it. The error is handled gracefully (logged, not fatal),
// and the plain-text Prowlarr/Torznab indexer search still works.
// A var, not a const, so tests can point it at an httptest server.
var torBoxSearchBaseURL = "https://search-api.torbox.app"

// torBoxValidationIMDbID (Fight Club) is what TorBox's own reference
// indexer definition uses to validate a key without requiring a real
// search query -- TestTorBoxIndexer mirrors that.
const torBoxValidationIMDbID = "tt0137523"

type torBoxSearchEnvelope struct {
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
	// Error is a distinct response shape TorBox's search-api uses for rate
	// limiting -- e.g. {"error": "Rate limit exceeded: 0 per 1 minute"},
	// with no "success"/"detail" fields at all (confirmed against both
	// production and AIOStreams' own TorBoxRateLimitErrorResponseSchema,
	// which models this exact shape as a known, expected case).
	Error string `json:"error"`
	Data  struct {
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
// at all: it's strictly GET /torrents/imdb_id:{id} for a movie, or
// .../torrents/imdb_id:{id}?season=S&episode=E for a TV episode. The
// imdb_id: prefix and explicit search_user_engines=false match TorBox's
// actively-maintained Stremio addon reference implementation
// (github.com/Viren070/AIOStreams) rather than an older Prowlarr indexer
// definition that used a bare "imdb:" prefix and omitted the param
// entirely -- confirmed empirically that both prefixes/param combinations
// return identical responses against a real account, so this is a
// correctness/future-proofing match, not a fix for any specific observed
// bug. There's no "whole season" query mode (season+episode are both
// required), so season-pack acquisition sends episode=1 as a
// representative probe -- many real season-pack releases are indexed
// under every episode they contain, so this often still surfaces one;
// scanner.LooksLikeSingleEpisode (already used by the NZB season-pack
// tier) filters the results the same way regardless of which tier found
// them.
func searchTorBoxIndexer(ctx context.Context, name, apiKey, imdbID string, season, episode int) ([]SearchResult, error) {
	q := url.Values{"search_user_engines": {"false"}, "check_owned": {"true"}, "metadata": {"false"}}
	path := fmt.Sprintf("/torrents/imdb_id:%s", imdbID)
	if season > 0 {
		if episode <= 0 {
			episode = 1
		}
		q.Set("season", fmt.Sprintf("%d", season))
		q.Set("episode", fmt.Sprintf("%d", episode))
	}
	path += "?" + q.Encode()

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
	if envelope.Error != "" {
		return nil, fmt.Errorf("torrent: TorBox indexer %s: %s", name, envelope.Error)
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

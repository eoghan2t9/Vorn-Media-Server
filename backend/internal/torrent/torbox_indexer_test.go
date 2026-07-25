package torrent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchTorBoxIndexer_Movie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/torrents/imdb:tt0137523") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"torrents": []map[string]any{
					{"raw_title": "Fight Club 1999 1080p BluRay x264-GROUP", "magnet": "magnet:?xt=urn:btih:FAKE1", "size": 2_000_000_000, "last_known_seeders": 42},
					{"raw_title": "no magnet, should be skipped", "magnet": "", "size": 100},
				},
			},
		})
	}))
	defer srv.Close()

	old := torBoxSearchBaseURL
	torBoxSearchBaseURL = srv.URL
	defer func() { torBoxSearchBaseURL = old }()

	results, err := searchTorBoxIndexer(context.Background(), "TorBox", "test-key", "tt0137523", 0, 0)
	if err != nil {
		t.Fatalf("searchTorBoxIndexer: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (magnet-less entry skipped), got %d: %+v", len(results), results)
	}
	got := results[0]
	if got.Title != "Fight Club 1999 1080p BluRay x264-GROUP" || got.DownloadURL != "magnet:?xt=urn:btih:FAKE1" || got.Seeders != 42 || got.SizeBytes != 2_000_000_000 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestSearchTorBoxIndexer_Episode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torrents/imdb:tt9288030" || r.URL.RawQuery != "season=2&episode=1" {
			t.Errorf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"torrents": []map[string]any{}},
		})
	}))
	defer srv.Close()

	old := torBoxSearchBaseURL
	torBoxSearchBaseURL = srv.URL
	defer func() { torBoxSearchBaseURL = old }()

	results, err := searchTorBoxIndexer(context.Background(), "TorBox", "test-key", "tt9288030", 2, 1)
	if err != nil {
		t.Fatalf("searchTorBoxIndexer: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestSearchTorBoxIndexer_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": false, "detail": "invalid api key"})
	}))
	defer srv.Close()

	old := torBoxSearchBaseURL
	torBoxSearchBaseURL = srv.URL
	defer func() { torBoxSearchBaseURL = old }()

	if _, err := searchTorBoxIndexer(context.Background(), "TorBox", "bad-key", "tt0137523", 0, 0); err == nil {
		t.Fatal("expected error for success:false response")
	}
}

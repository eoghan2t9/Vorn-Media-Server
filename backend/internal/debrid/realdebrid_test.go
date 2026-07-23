package debrid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// realDebridFake simulates the handful of Real-Debrid endpoints RealDebridClient
// calls, advancing a magnet through waiting_files_selection -> downloaded
// after selectFiles is called, so pollUntil is exercised for real.
type realDebridFake struct {
	selected bool
}

func (f *realDebridFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/torrents/addMagnet":
			json.NewEncoder(w).Encode(rdAddMagnetResponse{ID: "abc123"})
		case r.Method == http.MethodGet && r.URL.Path == "/torrents/info/abc123":
			status := "waiting_files_selection"
			var links []string
			if f.selected {
				status = "downloaded"
				links = []string{"https://real-debrid.com/d/FAKE1"}
			}
			json.NewEncoder(w).Encode(rdTorrentInfo{
				Status: status,
				Files:  []rdFile{{ID: 1, Path: "/Movie.2020.mkv", Bytes: 1000}},
				Links:  links,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/torrents/selectFiles/abc123":
			f.selected = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/unrestrict/link":
			json.NewEncoder(w).Encode(rdUnrestrictedLink{
				Filename: "Movie.2020.mkv",
				Filesize: 1000,
				Download: "https://real-debrid-cdn.com/FAKE1/Movie.2020.mkv",
			})
		default:
			http.NotFound(w, r)
		}
	}
}

func TestRealDebridClient_Resolve(t *testing.T) {
	fake := &realDebridFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewRealDebridClient()
	c.baseURL = srv.URL
	c.limiter = newLimiter(1_000_000) // don't let rate limiting slow the test

	files, err := c.Resolve(context.Background(), "test-key", "deadbeef")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	got := files[0]
	if got.Name != "Movie.2020.mkv" || got.SizeBytes != 1000 || got.StreamURL != "https://real-debrid-cdn.com/FAKE1/Movie.2020.mkv" {
		t.Fatalf("unexpected resolved file: %+v", got)
	}
}

func TestRealDebridClient_Resolve_TerminalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/torrents/addMagnet":
			json.NewEncoder(w).Encode(rdAddMagnetResponse{ID: "dead1"})
		case r.URL.Path == "/torrents/info/dead1":
			json.NewEncoder(w).Encode(rdTorrentInfo{Status: "dead"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewRealDebridClient()
	c.baseURL = srv.URL
	c.limiter = newLimiter(1_000_000)

	if _, err := c.Resolve(context.Background(), "test-key", "deadbeef"); err == nil {
		t.Fatal("expected an error for a dead torrent, got nil")
	}
}

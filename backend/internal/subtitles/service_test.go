package subtitles

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestService_Fetch_CachesOnDisk(t *testing.T) {
	var searchCalls, downloadCalls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login":
			json.NewEncoder(w).Encode(map[string]any{
				"token": "tok",
				"user":  map[string]any{"allowed_downloads": 5},
			})
		case r.URL.Path == "/subtitles":
			searchCalls.Add(1)
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"attributes": map[string]any{
						"release": "Test", "language": "en", "download_count": 1,
						"files": []map[string]any{{"file_id": 1, "file_name": "a.srt"}},
					}},
				},
			})
		case r.URL.Path == "/download":
			downloadCalls.Add(1)
			json.NewEncoder(w).Encode(map[string]any{
				"link":       srv.URL + "/dl",
				"remaining":  4,
				"reset_time": "24h",
			})
		case r.URL.Path == "/dl":
			w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	svc := &Service{
		client:    NewClient("test-key"),
		username:  "user",
		password:  "pass",
		cacheDir:  cacheDir,
		remaining: -1,
	}
	svc.client.baseURL = srv.URL

	videoPath := filepath.Join(t.TempDir(), "video.bin")
	if err := os.WriteFile(videoPath, make([]byte, hashMinSize), 0o644); err != nil {
		t.Fatal(err)
	}

	path1, err := svc.Fetch(t.Context(), videoPath, "en")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	content, err := os.ReadFile(path1)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got[:6] != "WEBVTT" {
		t.Errorf("cached file isn't WebVTT: %q", got)
	}

	quota := svc.Quota()
	if quota.Remaining != 4 || quota.ResetTime != "24h" {
		t.Errorf("Quota() = %+v, want Remaining=4 ResetTime=24h", quota)
	}

	// A second fetch for the same file must hit the cache, not the API.
	path2, err := svc.Fetch(t.Context(), videoPath, "en")
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if path1 != path2 {
		t.Errorf("expected the same cached path, got %q then %q", path1, path2)
	}
	if searchCalls.Load() != 1 || downloadCalls.Load() != 1 {
		t.Errorf("expected exactly 1 search + 1 download call across both fetches, got search=%d download=%d",
			searchCalls.Load(), downloadCalls.Load())
	}
}

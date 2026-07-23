package subtitles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_LoginSearchDownload(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The final CDN download link (unlike every real OpenSubtitles API
		// call) doesn't carry the Api-Key header, so that check only applies
		// to the api.opensubtitles.com-style endpoints below it.
		if r.URL.Path != "/dl/movie.srt" && r.Header.Get("Api-Key") != "test-api-key" {
			http.Error(w, "missing api key", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			json.NewEncoder(w).Encode(map[string]any{
				"token": "test-token",
				"user":  map[string]any{"allowed_downloads": 20},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/subtitles":
			if r.Header.Get("Authorization") != "test-token" {
				http.Error(w, "missing auth", http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("moviehash") != "abc123" {
				http.Error(w, "missing moviehash", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"attributes": map[string]any{
							"release":        "Movie.2020.1080p",
							"language":       "en",
							"download_count": 42,
							"files": []map[string]any{
								{"file_id": 555, "file_name": "movie.srt"},
							},
						},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/download":
			var body struct {
				FileID int `json:"file_id"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.FileID != 555 {
				http.Error(w, "wrong file id", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"link":       srv.URL + "/dl/movie.srt",
				"remaining":  19,
				"reset_time": "23h",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/dl/movie.srt":
			w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient("test-api-key")
	c.baseURL = srv.URL

	login, err := c.Login(context.Background(), "user", "pass")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if login.Token != "test-token" || login.AllowedDownloads != 20 {
		t.Fatalf("unexpected login result: %+v", login)
	}

	results, err := c.SearchByMovieHash(context.Background(), login.Token, "abc123", "en")
	if err != nil {
		t.Fatalf("SearchByMovieHash: %v", err)
	}
	if len(results) != 1 || results[0].FileID != 555 || results[0].DownloadCount != 42 {
		t.Fatalf("unexpected search results: %+v", results)
	}

	dl, err := c.Download(context.Background(), login.Token, results[0].FileID)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(dl.Content) != "1\n00:00:01,000 --> 00:00:02,000\nHello\n" {
		t.Errorf("unexpected content: %q", dl.Content)
	}
	if dl.Remaining != 19 {
		t.Errorf("Remaining = %d, want 19", dl.Remaining)
	}
}

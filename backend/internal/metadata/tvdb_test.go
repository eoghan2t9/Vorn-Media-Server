package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTVDbProvider_MatchSeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			json.NewEncoder(w).Encode(tvdbLoginResponse{Data: struct {
				Token string `json:"token"`
			}{Token: "fake-jwt"}})
		case r.Method == http.MethodGet && r.URL.Path == "/search":
			if r.Header.Get("Authorization") != "Bearer fake-jwt" {
				http.Error(w, "missing auth", http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("type") != "series" {
				t.Errorf("expected type=series, got %s", r.URL.Query().Get("type"))
			}
			json.NewEncoder(w).Encode(tvdbSearchResponse{
				Data: []tvdbSearchResult{
					{TVDbID: "121361", Name: "Game of Thrones", Overview: "...", FirstAirTime: "2011-04-17", ImageURL: "https://tvdb.example/poster.jpg"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewTVDbProvider("test-key", "")
	p.client.baseURL = srv.URL
	p.client.client = srv.Client()

	match, err := p.MatchSeries(context.Background(), "Game of Thrones")
	if err != nil {
		t.Fatalf("MatchSeries: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match, got nil")
	}
	if match.Title != "Game of Thrones" || match.TVDbID != 121361 || match.ReleaseDate != "2011-04-17" {
		t.Fatalf("unexpected match: %+v", match)
	}
}

func TestTVDbClient_RelogsInOn401(t *testing.T) {
	logins := 0
	searches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login":
			logins++
			json.NewEncoder(w).Encode(tvdbLoginResponse{Data: struct {
				Token string `json:"token"`
			}{Token: "token-v" + string(rune('0'+logins))}})
		case r.URL.Path == "/search":
			searches++
			// Reject the first token to simulate an expired/invalid cached
			// token, succeed on the retry's fresh one.
			if searches == 1 {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(tvdbSearchResponse{Data: []tvdbSearchResult{{TVDbID: "1", Name: "X"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewTVDbClient("test-key", "")
	c.baseURL = srv.URL
	c.client = srv.Client()
	c.token = "stale-token"

	result, err := c.SearchSeries(context.Background(), "X")
	if err != nil {
		t.Fatalf("SearchSeries: %v", err)
	}
	if result == nil || result.Name != "X" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if logins != 1 {
		t.Fatalf("expected exactly 1 re-login, got %d", logins)
	}
	if searches != 2 {
		t.Fatalf("expected exactly 2 search attempts, got %d", searches)
	}
}

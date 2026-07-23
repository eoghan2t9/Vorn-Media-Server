package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *TMDbClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &TMDbClient{apiKey: "test-key", baseURL: srv.URL, client: srv.Client()}
}

func TestSearchMovie(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/movie") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("api_key") != "test-key" {
			t.Errorf("expected api_key to be set, got query: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("query") != "Inception" {
			t.Errorf("expected query=Inception, got %s", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("year") != "2010" {
			t.Errorf("expected year=2010, got %s", r.URL.Query().Get("year"))
		}
		json.NewEncoder(w).Encode(tmdbSearchResponse[tmdbMovieResult]{
			Results: []tmdbMovieResult{
				{ID: 27205, Title: "Inception", Overview: "A thief...", ReleaseDate: "2010-07-15", PosterPath: "/poster.jpg"},
			},
		})
	})

	result, err := client.SearchMovie(context.Background(), "Inception", 2010)
	if err != nil {
		t.Fatalf("SearchMovie: %v", err)
	}
	if result == nil || result.ID != 27205 || result.Title != "Inception" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSearchMovieNoResults(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tmdbSearchResponse[tmdbMovieResult]{})
	})

	result, err := client.SearchMovie(context.Background(), "Nonexistent Movie XYZ", 0)
	if err != nil {
		t.Fatalf("SearchMovie: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
}

func TestSearchMovieHTTPError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := client.SearchMovie(context.Background(), "Inception", 2010)
	if err == nil {
		t.Fatal("expected an error for HTTP 401, got nil")
	}
}

func TestTrailerURL(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/movie/27205/videos") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(tmdbVideosResponse{
			Results: []struct {
				Site string `json:"site"`
				Type string `json:"type"`
				Key  string `json:"key"`
			}{
				{Site: "YouTube", Type: "Teaser", Key: "teaser123"},
				{Site: "YouTube", Type: "Trailer", Key: "trailer456"},
			},
		})
	})

	url, err := client.trailerURL(context.Background(), "movie", 27205)
	if err != nil {
		t.Fatalf("trailerURL: %v", err)
	}
	if url != "https://www.youtube.com/watch?v=trailer456" {
		t.Fatalf("expected trailer URL, got %q", url)
	}
}

func TestImageURL(t *testing.T) {
	if got := imageURL(""); got != "" {
		t.Errorf("imageURL(\"\") = %q, want empty", got)
	}
	if got := imageURL("/abc.jpg"); got != "https://image.tmdb.org/t/p/w500/abc.jpg" {
		t.Errorf("imageURL(/abc.jpg) = %q", got)
	}
}

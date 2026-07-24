package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newFanartTestClient(t *testing.T, handler http.HandlerFunc) *FanartClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &FanartClient{apiKey: "test-key", baseURL: srv.URL, client: srv.Client()}
}

func TestFanartMovieArt_PrefersEnglish(t *testing.T) {
	client := newFanartTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movies/27205" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(fanartMovieResponse{
			MoviePoster: []fanartArt{
				{URL: "https://fanart.tv/de-poster.jpg", Lang: "de"},
				{URL: "https://fanart.tv/en-poster.jpg", Lang: "en"},
			},
			MovieBackground: []fanartArt{{URL: "https://fanart.tv/bg.jpg", Lang: "00"}},
			HDMovieLogo:     []fanartArt{{URL: "https://fanart.tv/logo.png", Lang: "en"}},
		})
	})

	poster, backdrop, logo, err := client.MovieArt(context.Background(), 27205)
	if err != nil {
		t.Fatalf("MovieArt: %v", err)
	}
	if poster != "https://fanart.tv/en-poster.jpg" {
		t.Errorf("expected English poster preferred, got %q", poster)
	}
	if backdrop != "https://fanart.tv/bg.jpg" {
		t.Errorf("unexpected backdrop: %q", backdrop)
	}
	if logo != "https://fanart.tv/logo.png" {
		t.Errorf("unexpected logo: %q", logo)
	}
}

func TestFanartSeriesArt_NoResults(t *testing.T) {
	client := newFanartTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/121361" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(fanartTVResponse{})
	})

	poster, backdrop, logo, err := client.SeriesArt(context.Background(), 121361)
	if err != nil {
		t.Fatalf("SeriesArt: %v", err)
	}
	if poster != "" || backdrop != "" || logo != "" {
		t.Errorf("expected all-empty art, got poster=%q backdrop=%q logo=%q", poster, backdrop, logo)
	}
}

func TestPickArt(t *testing.T) {
	if got := pickArt(nil); got != "" {
		t.Errorf("pickArt(nil) = %q, want empty", got)
	}
	list := []fanartArt{{URL: "a", Lang: "fr"}, {URL: "b", Lang: "00"}}
	if got := pickArt(list); got != "b" {
		t.Errorf("expected textless fallback %q, got %q", "b", got)
	}
}

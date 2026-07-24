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

func TestPopularMovies(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/movie/popular") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") != "2" {
			t.Errorf("expected page=2, got %s", r.URL.Query().Get("page"))
		}
		json.NewEncoder(w).Encode(tmdbPagedResponse[tmdbMovieResult]{
			Page:       2,
			TotalPages: 10,
			Results: []tmdbMovieResult{
				{ID: 27205, Title: "Inception", Overview: "A thief...", ReleaseDate: "2010-07-15", PosterPath: "/poster.jpg"},
			},
		})
	})

	got, err := client.PopularMovies(context.Background(), 2)
	if err != nil {
		t.Fatalf("PopularMovies: %v", err)
	}
	if got.Page != 2 || got.TotalPages != 10 {
		t.Fatalf("unexpected pagination: %+v", got)
	}
	if len(got.Results) != 1 || got.Results[0].TmdbID != 27205 || got.Results[0].PosterURL != "https://image.tmdb.org/t/p/w500/poster.jpg" {
		t.Fatalf("unexpected results: %+v", got.Results)
	}
}

func TestPopularSeries(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/tv/popular") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(tmdbPagedResponse[tmdbTVResult]{
			Page: 1, TotalPages: 5,
			Results: []tmdbTVResult{{ID: 1396, Name: "Breaking Bad", FirstAirDate: "2008-01-20"}},
		})
	})

	got, err := client.PopularSeries(context.Background(), 1)
	if err != nil {
		t.Fatalf("PopularSeries: %v", err)
	}
	if len(got.Results) != 1 || got.Results[0].Title != "Breaking Bad" {
		t.Fatalf("unexpected results: %+v", got.Results)
	}
}

func TestTrendingMovies(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/trending/movie/week") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(tmdbPagedResponse[tmdbMovieResult]{Page: 1, TotalPages: 1})
	})
	if _, err := client.TrendingMovies(context.Background(), 0); err != nil {
		t.Fatalf("TrendingMovies: %v", err)
	}
}

func TestTrendingSeries(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/trending/tv/week") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(tmdbPagedResponse[tmdbTVResult]{Page: 1, TotalPages: 1})
	})
	if _, err := client.TrendingSeries(context.Background(), 0); err != nil {
		t.Fatalf("TrendingSeries: %v", err)
	}
}

func TestGetMovieDetails(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/movie/27205") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(tmdbMovieDetailsResult{
			ID: 27205, Title: "Inception", Overview: "A thief...", ReleaseDate: "2010-07-15",
			PosterPath: "/poster.jpg", BackdropPath: "/backdrop.jpg",
		})
	})

	got, err := client.GetMovieDetails(context.Background(), 27205)
	if err != nil {
		t.Fatalf("GetMovieDetails: %v", err)
	}
	if got.Title != "Inception" || got.BackdropURL != "https://image.tmdb.org/t/p/w500/backdrop.jpg" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestGetSeriesDetails(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tv/1396") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(tmdbSeriesDetailsResult{
			ID: 1396, Name: "Breaking Bad", FirstAirDate: "2008-01-20",
			Seasons: []struct {
				SeasonNumber int `json:"season_number"`
				EpisodeCount int `json:"episode_count"`
			}{
				{SeasonNumber: 0, EpisodeCount: 3},
				{SeasonNumber: 1, EpisodeCount: 7},
				{SeasonNumber: 2, EpisodeCount: 13},
			},
		})
	})

	got, err := client.GetSeriesDetails(context.Background(), 1396)
	if err != nil {
		t.Fatalf("GetSeriesDetails: %v", err)
	}
	if len(got.Seasons) != 2 || got.Seasons[0].SeasonNumber != 1 || got.Seasons[1].EpisodeCount != 13 {
		t.Fatalf("expected season 0 (specials) dropped, got: %+v", got.Seasons)
	}
}

func TestGetSeasonDetails(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tv/1396/season/1") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(tmdbSeasonResult{
			Episodes: []struct {
				SeasonNumber  int    `json:"season_number"`
				EpisodeNumber int    `json:"episode_number"`
				Name          string `json:"name"`
				Overview      string `json:"overview"`
				AirDate       string `json:"air_date"`
			}{
				{SeasonNumber: 1, EpisodeNumber: 1, Name: "Pilot", AirDate: "2008-01-20"},
				{SeasonNumber: 1, EpisodeNumber: 2, Name: "Cat's in the Bag...", AirDate: "2008-01-27"},
			},
		})
	})

	got, err := client.GetSeasonDetails(context.Background(), 1396, 1)
	if err != nil {
		t.Fatalf("GetSeasonDetails: %v", err)
	}
	if len(got) != 2 || got[0].Title != "Pilot" || got[1].EpisodeNumber != 2 {
		t.Fatalf("unexpected episodes: %+v", got)
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

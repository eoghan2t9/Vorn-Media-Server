package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newOMDbTestClient(t *testing.T, handler http.HandlerFunc) *OMDbClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &OMDbClient{apiKey: "test-key", baseURL: srv.URL, client: srv.Client()}
}

func TestOMDbRatingsByIMDbID(t *testing.T) {
	client := newOMDbTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("i") != "tt1375666" {
			t.Errorf("expected i=tt1375666, got %s", r.URL.Query().Get("i"))
		}
		if r.URL.Query().Get("apikey") != "test-key" {
			t.Errorf("expected apikey to be set")
		}
		json.NewEncoder(w).Encode(omdbResponse{
			Response: "True",
			Ratings: []struct {
				Source string `json:"Source"`
				Value  string `json:"Value"`
			}{
				{Source: "Internet Movie Database", Value: "8.8/10"},
				{Source: "Rotten Tomatoes", Value: "87%"},
				{Source: "Metacritic", Value: "74/100"},
			},
		})
	})

	imdbRating, rt, err := client.RatingsByIMDbID(context.Background(), "tt1375666")
	if err != nil {
		t.Fatalf("RatingsByIMDbID: %v", err)
	}
	if imdbRating != "8.8/10" || rt != "87%" {
		t.Fatalf("unexpected ratings: imdb=%q rt=%q", imdbRating, rt)
	}
}

func TestOMDbRatingsByIMDbID_NotFound(t *testing.T) {
	client := newOMDbTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(omdbResponse{Response: "False", Error: "Incorrect IMDb ID."})
	})

	if _, _, err := client.RatingsByIMDbID(context.Background(), "ttbogus"); err == nil {
		t.Fatal("expected an error for Response=False, got nil")
	}
}

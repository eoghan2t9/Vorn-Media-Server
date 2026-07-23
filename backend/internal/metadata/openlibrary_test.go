package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newOpenLibraryTestProvider(t *testing.T, handler http.HandlerFunc) *OpenLibraryProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &OpenLibraryProvider{baseURL: srv.URL, coversBaseURL: srv.URL, client: srv.Client()}
}

func TestOpenLibraryMatchBook(t *testing.T) {
	provider := newOpenLibraryTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("title"); got != "Test Book" {
			t.Errorf("title = %q", got)
		}
		if got := r.URL.Query().Get("author"); got != "Test Author" {
			t.Errorf("author = %q", got)
		}
		json.NewEncoder(w).Encode(olSearchResponse{
			Docs: []olDoc{{
				Key:              "/works/OL12345W",
				Title:            "Test Book",
				AuthorName:       []string{"Test Author"},
				FirstPublishYear: 2019,
				CoverID:          987,
			}},
		})
	})

	match, err := provider.MatchBook(context.Background(), "Test Book", "Test Author")
	if err != nil {
		t.Fatalf("MatchBook: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match, got nil")
	}
	if match.WorkKey != "/works/OL12345W" || match.Author != "Test Author" || match.ReleaseDate != "2019" {
		t.Errorf("unexpected match: %+v", match)
	}
	if match.PosterURL == "" {
		t.Error("expected a PosterURL when cover_i is present")
	}
}

func TestOpenLibraryMatchBookNoResults(t *testing.T) {
	provider := newOpenLibraryTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(olSearchResponse{})
	})

	match, err := provider.MatchBook(context.Background(), "Nonexistent", "Nobody")
	if err != nil {
		t.Fatalf("MatchBook: %v", err)
	}
	if match != nil {
		t.Fatalf("expected nil match, got %+v", match)
	}
}

func TestOpenLibraryMatchBookUnknownAuthorOmitted(t *testing.T) {
	provider := newOpenLibraryTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("author") {
			t.Errorf("expected no author param for the fallback 'Unknown Artist' sentinel, got %q", r.URL.Query().Get("author"))
		}
		json.NewEncoder(w).Encode(olSearchResponse{})
	})

	_, err := provider.MatchBook(context.Background(), "Some Book", "Unknown Artist")
	if err != nil {
		t.Fatalf("MatchBook: %v", err)
	}
}

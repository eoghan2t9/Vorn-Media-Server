package metadata

import (
	"context"
	"testing"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// TestProcessItemSkipsUnconfiguredProviders locks in that each kind is a
// no-op (not an error) when its provider isn't configured -- this is the
// behavior the music/audiobook metadata toggles rely on, and none of these
// paths touch svc.store, so they're testable without a live database.
func TestProcessItemSkipsUnconfiguredProviders(t *testing.T) {
	svc := &Service{} // no TMDb/music/audiobook provider at all

	cases := []struct {
		name string
		item *store.MediaItem
	}{
		{"movie with no TMDb provider", &store.MediaItem{Kind: "movie", Title: "X"}},
		{"series with no TMDb provider", &store.MediaItem{Kind: "series", Title: "X"}},
		{"album with no music provider", &store.MediaItem{Kind: "album", Title: "X", ParentID: strPtr("artist-id")}},
		{"album with no parent", &store.MediaItem{Kind: "album", Title: "X", ParentID: nil}},
		{"book with no audiobook provider", &store.MediaItem{Kind: "book", Title: "X"}},
		{"audiobook with no audiobook provider", &store.MediaItem{Kind: "audiobook", Title: "X"}},
		{"unhandled kind", &store.MediaItem{Kind: "track", Title: "X"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			matched, err := svc.processItem(context.Background(), c.item)
			if err != nil {
				t.Fatalf("processItem: %v", err)
			}
			if matched {
				t.Fatalf("expected matched=false with no provider configured")
			}
		})
	}
}

func TestParsePartialDate(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"2020-05-01", false},
		{"2020-05", false},
		{"2020", false},
		{"", true},
		{"not-a-date", true},
	}
	for _, c := range cases {
		_, err := parsePartialDate(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parsePartialDate(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
		}
	}
}

func strPtr(s string) *string { return &s }

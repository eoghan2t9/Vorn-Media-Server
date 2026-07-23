package metadata

import (
	"context"
	"testing"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// fakeMusicProvider/fakeAudiobookProvider report whether they were called,
// used to prove that the *enabled* flag (not just provider presence) is
// what gates album/book/audiobook processing -- the whole point of checking
// it live per sync run is that a configured-but-disabled provider must
// still be skipped. They return (nil, nil) -- a legitimate "found nothing"
// -- so the caller returns before ever touching svc.store (which is nil in
// this test; the full apply-to-DB path is covered by the end-to-end
// scratch-stack verification instead).
type fakeMusicProvider struct{ called bool }

func (p *fakeMusicProvider) MatchAlbum(context.Context, string, string) (*MusicMatch, error) {
	p.called = true
	return nil, nil
}

type fakeAudiobookProvider struct{ called bool }

func (p *fakeAudiobookProvider) MatchBook(context.Context, string, string) (*BookMatch, error) {
	p.called = true
	return nil, nil
}

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
			matched, err := svc.processItem(context.Background(), c.item, true, true)
			if err != nil {
				t.Fatalf("processItem: %v", err)
			}
			if matched {
				t.Fatalf("expected matched=false with no provider configured")
			}
		})
	}
}

// TestProcessItemRespectsLiveEnabledFlag proves the enabled flags -- not
// just provider presence -- gate album/book/audiobook processing, which is
// what makes the Admin > Integrations toggle take effect without a server
// restart (see Service.run, which reads these flags fresh every sync run).
func TestProcessItemRespectsLiveEnabledFlag(t *testing.T) {
	music := &fakeMusicProvider{}
	book := &fakeAudiobookProvider{}
	svc := &Service{musicProvider: music, audiobookProvider: book}

	album := &store.MediaItem{Kind: "album", Title: "X", ParentID: nil} // no parent -> never calls the store
	if matched, err := svc.processItem(context.Background(), album, false, true); err != nil || matched {
		t.Fatalf("expected album to be skipped while musicEnabled=false, got matched=%v err=%v", matched, err)
	}
	if music.called {
		t.Fatal("provider should not have been called while disabled")
	}

	bookItem := &store.MediaItem{Kind: "book", Title: "X"}
	if matched, err := svc.processItem(context.Background(), bookItem, true, false); err != nil || matched {
		t.Fatalf("expected book to be skipped while audiobookEnabled=false, got matched=%v err=%v", matched, err)
	}
	if book.called {
		t.Fatal("provider should not have been called while disabled")
	}

	// Enabling flips the gate open and the provider does get called -- the
	// call itself would then try to write the match via svc.store, which is
	// nil here (no live DB in this test), so this only asserts up to the
	// provider invocation, not the full apply-to-DB path (that's covered by
	// the end-to-end scratch-stack verification instead).
	_, _ = svc.processItem(context.Background(), bookItem, true, true)
	if !book.called {
		t.Fatal("expected the provider to be called once enabled")
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

package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newMusicBrainzTestProvider(t *testing.T, mbHandler, caaHandler http.HandlerFunc) *MusicBrainzProvider {
	t.Helper()
	mbSrv := httptest.NewServer(mbHandler)
	t.Cleanup(mbSrv.Close)
	caaSrv := httptest.NewServer(caaHandler)
	t.Cleanup(caaSrv.Close)

	return &MusicBrainzProvider{baseURL: mbSrv.URL, coverArtBaseURL: caaSrv.URL, client: mbSrv.Client()}
}

func TestMusicBrainzMatchAlbum(t *testing.T) {
	provider := newMusicBrainzTestProvider(t,
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("query"); got != "artist:Test Artist AND release:Test Album" {
				t.Errorf("unexpected query: %s", got)
			}
			json.NewEncoder(w).Encode(mbReleaseSearchResponse{
				Releases: []mbRelease{{
					ID:    "11111111-1111-1111-1111-111111111111",
					Title: "Test Album",
					Date:  "2020-05-01",
					ArtistCredit: []struct {
						Name string `json:"name"`
					}{{Name: "Test Artist"}},
				}},
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(mbCoverArtInfo{
				Images: []struct {
					Front bool `json:"front"`
				}{{Front: true}},
			})
		},
	)

	match, err := provider.MatchAlbum(context.Background(), "Test Artist", "Test Album")
	if err != nil {
		t.Fatalf("MatchAlbum: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match, got nil")
	}
	if match.ReleaseMBID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("ReleaseMBID = %q", match.ReleaseMBID)
	}
	if match.ArtistName != "Test Artist" {
		t.Errorf("ArtistName = %q", match.ArtistName)
	}
	if match.PosterURL == "" {
		t.Error("expected a PosterURL when cover art archive reports a front image")
	}
}

func TestMusicBrainzMatchAlbumNoResults(t *testing.T) {
	provider := newMusicBrainzTestProvider(t,
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(mbReleaseSearchResponse{})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	)

	match, err := provider.MatchAlbum(context.Background(), "Nobody", "Nothing")
	if err != nil {
		t.Fatalf("MatchAlbum: %v", err)
	}
	if match != nil {
		t.Fatalf("expected nil match, got %+v", match)
	}
}

func TestMusicBrainzMatchAlbumNoCoverArt(t *testing.T) {
	provider := newMusicBrainzTestProvider(t,
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(mbReleaseSearchResponse{
				Releases: []mbRelease{{ID: "abc", Title: "Album", Date: "2020"}},
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	)

	match, err := provider.MatchAlbum(context.Background(), "Artist", "Album")
	if err != nil {
		t.Fatalf("MatchAlbum: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match even with no cover art")
	}
	if match.PosterURL != "" {
		t.Errorf("expected empty PosterURL when Cover Art Archive 404s, got %q", match.PosterURL)
	}
}

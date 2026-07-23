package plex

import (
	"testing"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

func TestMillisRoundTrip(t *testing.T) {
	if got := SecondsToMillis(1.5); got != 1500 {
		t.Errorf("SecondsToMillis(1.5) = %d, want 1500", got)
	}
	if got := MillisToSeconds(1500); got != 1.5 {
		t.Errorf("MillisToSeconds(1500) = %v, want 1.5", got)
	}
	if got := SecondsToMillis(0); got != 0 {
		t.Errorf("SecondsToMillis(0) = %d, want 0", got)
	}
}

func TestDirectoryFromLibrary(t *testing.T) {
	movies := &store.Library{ID: "lib-1", Name: "Movies", Type: "movie"}
	d := DirectoryFromLibrary(movies)
	if d.Type != "movie" || d.Key != "lib-1" {
		t.Errorf("DirectoryFromLibrary(movie) = %+v", d)
	}

	series := &store.Library{ID: "lib-2", Name: "Shows", Type: "series"}
	d = DirectoryFromLibrary(series)
	if d.Type != "show" {
		t.Errorf("DirectoryFromLibrary(series).Type = %q, want show", d.Type)
	}
}

func TestMetadataFromItem_Episode(t *testing.T) {
	seasonID := "season-1"
	season := 2
	episode := 5
	released := time.Date(2021, 3, 1, 0, 0, 0, 0, time.UTC)

	m := &store.MediaItem{
		ID:            "ep-1",
		ParentID:      &seasonID,
		Kind:          "episode",
		Title:         "S02E05",
		SeasonNumber:  &season,
		EpisodeNumber: &episode,
		ReleaseDate:   &released,
		AddedAt:       time.Now(),
	}

	md := MetadataFromItem(m, 1200, "https://example.com/poster.jpg", "", nil)
	if md.Type != "episode" {
		t.Errorf("Type = %q, want episode", md.Type)
	}
	if md.ParentRatingKey != seasonID {
		t.Errorf("ParentRatingKey = %q, want %q", md.ParentRatingKey, seasonID)
	}
	if md.Index == nil || *md.Index != episode {
		t.Errorf("Index = %v, want %d", md.Index, episode)
	}
	if md.ParentIndex == nil || *md.ParentIndex != season {
		t.Errorf("ParentIndex = %v, want %d", md.ParentIndex, season)
	}
	if md.Duration != 1_200_000 {
		t.Errorf("Duration = %d, want 1200000ms", md.Duration)
	}
	if md.Thumb == "" {
		t.Errorf("expected a Thumb path when posterURL is set")
	}
	if md.Year == nil || *md.Year != 2021 {
		t.Errorf("Year = %v, want 2021", md.Year)
	}
}

func TestMetadataFromItem_ViewCount(t *testing.T) {
	m := &store.MediaItem{ID: "movie-1", Kind: "movie", Title: "A Movie", AddedAt: time.Now()}

	nearlyDone := &store.PlaybackState{PositionSeconds: 95, DurationSeconds: 100}
	md := MetadataFromItem(m, 100, "", "", nearlyDone)
	if md.ViewCount != 1 {
		t.Errorf("expected ViewCount = 1 at 95%% progress, got %d", md.ViewCount)
	}
	if md.ViewOffset != 95_000 {
		t.Errorf("ViewOffset = %d, want 95000ms", md.ViewOffset)
	}

	justStarted := &store.PlaybackState{PositionSeconds: 5, DurationSeconds: 100}
	md = MetadataFromItem(m, 100, "", "", justStarted)
	if md.ViewCount != 0 {
		t.Errorf("expected ViewCount = 0 at 5%% progress, got %d", md.ViewCount)
	}
}

package jellyfin

import (
	"testing"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

func TestTicksRoundTrip(t *testing.T) {
	if got := SecondsToTicks(1.5); got != 15_000_000 {
		t.Errorf("SecondsToTicks(1.5) = %d, want 15000000", got)
	}
	if got := TicksToSeconds(15_000_000); got != 1.5 {
		t.Errorf("TicksToSeconds(15000000) = %v, want 1.5", got)
	}
	if got := SecondsToTicks(0); got != 0 {
		t.Errorf("SecondsToTicks(0) = %d, want 0", got)
	}
}

func TestLibraryItem(t *testing.T) {
	movies := &store.Library{ID: "lib-1", Name: "Movies", Type: "movie"}
	dto := LibraryItem(movies, "server-1")
	if dto.Type != "CollectionFolder" || dto.CollectionType != "movies" || !dto.IsFolder {
		t.Errorf("LibraryItem(movie) = %+v", dto)
	}

	series := &store.Library{ID: "lib-2", Name: "Shows", Type: "series"}
	dto = LibraryItem(series, "server-1")
	if dto.CollectionType != "tvshows" {
		t.Errorf("LibraryItem(series).CollectionType = %q, want tvshows", dto.CollectionType)
	}
}

func TestMediaItem_Episode(t *testing.T) {
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

	dto := MediaItem(m, "server-1", "https://example.com/poster.jpg", "", nil)
	if dto.Type != "Episode" || dto.IsFolder {
		t.Errorf("MediaItem(episode) type/folder = %q/%v", dto.Type, dto.IsFolder)
	}
	if dto.ParentId != seasonID {
		t.Errorf("ParentId = %q, want %q", dto.ParentId, seasonID)
	}
	if dto.IndexNumber == nil || *dto.IndexNumber != episode {
		t.Errorf("IndexNumber = %v, want %d", dto.IndexNumber, episode)
	}
	if dto.ParentIndexNumber == nil || *dto.ParentIndexNumber != season {
		t.Errorf("ParentIndexNumber = %v, want %d", dto.ParentIndexNumber, season)
	}
	if dto.ProductionYear == nil || *dto.ProductionYear != 2021 {
		t.Errorf("ProductionYear = %v, want 2021", dto.ProductionYear)
	}
	if dto.ImageTags["Primary"] == "" {
		t.Errorf("expected a Primary image tag when posterURL is set")
	}
	if dto.UserData == nil || dto.UserData.Played {
		t.Errorf("UserData = %+v, want non-nil and unplayed with no playback state", dto.UserData)
	}
}

func TestMediaItem_PlaybackState(t *testing.T) {
	m := &store.MediaItem{ID: "movie-1", Kind: "movie", Title: "A Movie", AddedAt: time.Now()}

	nearlyDone := &store.PlaybackState{PositionSeconds: 95, DurationSeconds: 100}
	dto := MediaItem(m, "server-1", "", "", nearlyDone)
	if !dto.UserData.Played {
		t.Errorf("expected Played = true at 95%% progress")
	}
	if dto.UserData.PlaybackPositionTicks != SecondsToTicks(95) {
		t.Errorf("PlaybackPositionTicks = %d, want %d", dto.UserData.PlaybackPositionTicks, SecondsToTicks(95))
	}

	justStarted := &store.PlaybackState{PositionSeconds: 5, DurationSeconds: 100}
	dto = MediaItem(m, "server-1", "", "", justStarted)
	if dto.UserData.Played {
		t.Errorf("expected Played = false at 5%% progress")
	}
}

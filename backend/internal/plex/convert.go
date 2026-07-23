package plex

import (
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// SecondsToMillis/MillisToSeconds convert between Vorn's float-seconds
// duration/position tracking and Plex's integer-milliseconds convention.
func SecondsToMillis(seconds float64) int64 {
	if seconds <= 0 {
		return 0
	}
	return int64(seconds * 1000)
}

func MillisToSeconds(ms int64) float64 {
	return float64(ms) / 1000
}

func itemType(kind string) string {
	switch kind {
	case "movie":
		return "movie"
	case "series":
		return "show"
	case "season":
		return "season"
	case "episode":
		return "episode"
	default:
		return "clip"
	}
}

// DirectoryFromLibrary converts a Vorn library into a Plex library section.
func DirectoryFromLibrary(l *store.Library) Directory {
	sectionType := "show"
	switch l.Type {
	case "movie":
		sectionType = "movie"
	case "music", "audiobook":
		sectionType = "artist"
	}
	return Directory{
		Key:   l.ID,
		Title: l.Name,
		Type:  sectionType,
	}
}

// MetadataFromItem converts a Vorn media item into Plex Metadata. Duration
// comes from durationSeconds (0 if unknown -- e.g. list views that don't
// probe every file) rather than being derived from playback, since Plex
// distinguishes Duration (the file's length) from ViewOffset (progress).
func MetadataFromItem(m *store.MediaItem, durationSeconds float64, posterURL, backdropURL string, playback *store.PlaybackState) Metadata {
	md := Metadata{
		RatingKey: m.ID,
		Key:       "/library/metadata/" + m.ID,
		Title:     m.Title,
		Type:      itemType(m.Kind),
		Summary:   m.Overview,
		AddedAt:   m.AddedAt.Unix(),
		Duration:  SecondsToMillis(durationSeconds),
	}
	if m.ParentID != nil {
		md.ParentRatingKey = *m.ParentID
	}
	if m.SeasonNumber != nil {
		md.ParentIndex = m.SeasonNumber
	}
	if m.EpisodeNumber != nil {
		md.Index = m.EpisodeNumber
	}
	if m.ReleaseDate != nil {
		year := m.ReleaseDate.Year()
		md.Year = &year
		md.OriginallyAvailableAt = m.ReleaseDate.Format("2006-01-02")
	}
	if posterURL != "" {
		md.Thumb = "/library/metadata/" + m.ID + "/thumb"
	}
	if backdropURL != "" {
		md.Art = "/library/metadata/" + m.ID + "/art"
	}
	if playback != nil {
		md.ViewOffset = SecondsToMillis(playback.PositionSeconds)
		if playback.DurationSeconds > 0 && playback.PositionSeconds >= playback.DurationSeconds*0.95 {
			md.ViewCount = 1
		}
	}
	return md
}

// MediaFromItem builds the Media/Part hierarchy a /library/metadata/{id}
// detail response needs to point a client at a playable stream. partKey is
// the URL the client should fetch the actual bytes from.
func MediaFromItem(m *store.MediaItem, durationSeconds float64, container, videoCodec, audioCodec string, width, height int, partKey string) Media {
	return Media{
		ID:         1,
		Duration:   SecondsToMillis(durationSeconds),
		Container:  container,
		VideoCodec: videoCodec,
		AudioCodec: audioCodec,
		Width:      width,
		Height:     height,
		Part: []Part{{
			ID:        1,
			Key:       partKey,
			Duration:  SecondsToMillis(durationSeconds),
			Container: container,
		}},
	}
}

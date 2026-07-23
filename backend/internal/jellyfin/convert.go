package jellyfin

import (
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

func itemType(kind string) string {
	switch kind {
	case "movie":
		return "Movie"
	case "series":
		return "Series"
	case "season":
		return "Season"
	case "episode":
		return "Episode"
	default:
		return "Folder"
	}
}

// LibraryItem converts a Vorn library into a Jellyfin "view" (a top-level
// CollectionFolder shown on the client's home screen).
func LibraryItem(l *store.Library, serverID string) BaseItemDto {
	collectionType := "tvshows"
	switch l.Type {
	case "movie":
		collectionType = "movies"
	case "music":
		collectionType = "music"
	case "audiobook":
		collectionType = "books"
	}
	return BaseItemDto{
		Id:             l.ID,
		ServerId:       serverID,
		Name:           l.Name,
		Type:           "CollectionFolder",
		CollectionType: collectionType,
		IsFolder:       true,
	}
}

// MediaItem converts a Vorn media item into a BaseItemDto. posterURL/
// backdropURL come from the item's metadata blob (not a MediaItem field) and
// only control whether ImageTags/BackdropImageTags advertise an image --
// Jellyfin clients don't validate the tag value itself, just its presence.
func MediaItem(m *store.MediaItem, serverID, posterURL, backdropURL string, playback *store.PlaybackState) BaseItemDto {
	dto := BaseItemDto{
		Id:                m.ID,
		ServerId:          serverID,
		Name:              m.Title,
		SortName:          m.SortTitle,
		Overview:          m.Overview,
		Type:              itemType(m.Kind),
		IsFolder:          m.Kind == "series" || m.Kind == "season",
		MediaType:         "Video",
		DateCreated:       m.AddedAt.Format(time.RFC3339),
		IndexNumber:       m.EpisodeNumber,
		ParentIndexNumber: m.SeasonNumber,
	}
	if m.ParentID != nil {
		dto.ParentId = *m.ParentID
	}
	if m.ReleaseDate != nil {
		year := m.ReleaseDate.Year()
		dto.ProductionYear = &year
		dto.PremiereDate = m.ReleaseDate.Format(time.RFC3339)
	}
	if posterURL != "" {
		dto.ImageTags = map[string]string{"Primary": "poster"}
	}
	if backdropURL != "" {
		dto.BackdropImageTags = []string{"backdrop"}
	}

	dto.UserData = &UserItemData{}
	if playback != nil {
		dto.UserData.PlaybackPositionTicks = SecondsToTicks(playback.PositionSeconds)
		dto.UserData.Played = playback.DurationSeconds > 0 && playback.PositionSeconds >= playback.DurationSeconds*0.95
	}
	return dto
}

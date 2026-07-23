package debrid

import (
	"log"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// PromoteCompleted turns a resolved debrid item's files into browsable
// media_items, same as the torrent/NZB auto-add watchers. Unlike those,
// there is no local download step: each media item's path is set directly
// to the provider-hosted stream URL, so playback streams from the debrid
// provider's CDN with no local storage involved.
func PromoteCompleted(st *store.Store, item *store.DebridItem) {
	if item.LibraryID == nil {
		log.Printf("debrid: %s (%s) resolved with no destination library; skipping auto-add", item.ID, item.Name)
		return
	}
	if item.Promoted {
		return
	}

	files, err := st.ListDebridFiles(item.ID)
	if err != nil {
		log.Printf("debrid: listing files for %s: %v", item.ID, err)
		return
	}

	for _, f := range files {
		if !scanner.IsVideoFile(f.Name) {
			continue
		}
		parsed := scanner.ParseFilename(f.Name)
		var promoteErr error
		switch parsed.Kind {
		case "movie":
			_, promoteErr = st.PromoteMovie(store.PromoteMovieInput{
				LibraryID: *item.LibraryID,
				Title:     parsed.Title,
				Year:      yearPtr(parsed.Year),
				Path:      f.StreamURL,
			})
		case "episode":
			_, promoteErr = st.PromoteEpisode(store.PromoteEpisodeInput{
				LibraryID:     *item.LibraryID,
				SeriesTitle:   parsed.Title,
				SeasonNumber:  parsed.SeasonNumber,
				EpisodeNumber: parsed.EpisodeNumber,
				Path:          f.StreamURL,
			})
		default:
			continue
		}
		if promoteErr != nil {
			log.Printf("debrid: promoting %s: %v", f.Name, promoteErr)
		}
	}

	if err := st.MarkDebridItemPromoted(item.ID); err != nil {
		log.Printf("debrid: marking %s promoted: %v", item.ID, err)
	}
}

func yearPtr(y int) *int {
	if y == 0 {
		return nil
	}
	return &y
}

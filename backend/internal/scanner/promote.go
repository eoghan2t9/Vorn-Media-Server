package scanner

import (
	"log"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// promoteScanFiles turns not-yet-matched scan_files rows into browsable
// media_items (movie, or series/season/episode) using only the filename
// heuristics already computed during scanning. This is deliberately a rough
// first pass: Phase 4's metadata matching enriches these same rows with
// real titles/art/overviews from TMDb rather than creating new ones.
func promoteScanFiles(st *store.Store, libraryID string) error {
	pending, err := st.ListUnmatchedScanFiles(libraryID)
	if err != nil {
		return err
	}

	for _, f := range pending {
		var mediaItemID string
		var err error

		switch f.GuessedKind {
		case "movie":
			mediaItemID, err = st.PromoteMovie(store.PromoteMovieInput{
				LibraryID: libraryID,
				Title:     f.GuessedTitle,
				Year:      f.GuessedYear,
				Path:      f.Path,
			})
		case "episode":
			if f.SeasonNumber == nil || f.EpisodeNumber == nil {
				continue
			}
			mediaItemID, err = st.PromoteEpisode(store.PromoteEpisodeInput{
				LibraryID:     libraryID,
				SeriesTitle:   f.GuessedTitle,
				SeasonNumber:  *f.SeasonNumber,
				EpisodeNumber: *f.EpisodeNumber,
				Path:          f.Path,
			})
		default:
			continue
		}

		if err != nil {
			log.Printf("scanner: promoting scan file %s: %v", f.ID, err)
			continue
		}
		if err := st.MarkScanFilePromoted(f.ID, mediaItemID); err != nil {
			log.Printf("scanner: marking scan file %s promoted: %v", f.ID, err)
		}
	}
	return nil
}

package scanner

import (
	"log"
	"sort"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// promoteScanFiles turns not-yet-matched scan_files rows into browsable
// media_items (movie, or series/season/episode, or artist/album/track, or
// audiobook/book/chapter) using only the filename/tag heuristics already
// computed during scanning. This is deliberately a rough first pass: Phase
// 4's metadata matching enriches movie/series rows with real titles/art from
// TMDb rather than creating new ones (there's no equivalent enrichment step
// for music/audiobooks yet).
func promoteScanFiles(st *store.Store, libraryID string) error {
	pending, err := st.ListUnmatchedScanFiles(libraryID)
	if err != nil {
		return err
	}

	// "chapter"-guessed files (every audiobook file, regardless of whether it
	// ends up flat or nested) need to be grouped by book before we know
	// whether to promote a flat "audiobook" item or a "book" parent with
	// "chapter" children -- handled as a separate pass below instead of the
	// simple one-file-at-a-time loop.
	var chapterFiles []*store.UnmatchedScanFile

	for _, f := range pending {
		if f.GuessedKind == "chapter" {
			chapterFiles = append(chapterFiles, f)
			continue
		}

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
		case "track":
			mediaItemID, err = st.PromoteTrack(store.PromoteTrackInput{
				LibraryID:   libraryID,
				Artist:      f.GuessedArtist,
				Album:       f.GuessedAlbum,
				Title:       f.GuessedTitle,
				TrackNumber: derefOrZero(f.EpisodeNumber),
				Path:        f.Path,
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

	promoteAudiobookChapters(st, libraryID, chapterFiles)
	return nil
}

// promoteAudiobookChapters groups pending audiobook files by their guessed
// book title (guessed_album): a group of one becomes a single flat
// "audiobook" item, a group of more than one becomes a "book" parent with
// "chapter" children ordered by tag-provided track number, falling back to
// filename order when a file has no track number at all.
//
// Known limitation: this only groups files discovered together in the same
// promotion pass. A chapter added to a book in a later, separate scan (after
// the first file was already promoted as a flat "audiobook") is promoted as
// its own second flat item rather than retroactively converted into a
// two-chapter book -- audiobooks are typically added as a complete folder
// before the first scan, so this is treated as an acceptable gap rather than
// building full retroactive re-grouping.
func promoteAudiobookChapters(st *store.Store, libraryID string, files []*store.UnmatchedScanFile) {
	groups := make(map[string][]*store.UnmatchedScanFile)
	for _, f := range files {
		groups[f.GuessedAlbum] = append(groups[f.GuessedAlbum], f)
	}

	for bookTitle, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			ni, nj := derefOrZero(group[i].EpisodeNumber), derefOrZero(group[j].EpisodeNumber)
			if ni != nj {
				return ni < nj
			}
			return group[i].Path < group[j].Path
		})

		if len(group) == 1 {
			f := group[0]
			title := f.GuessedTitle
			if title == "" {
				title = bookTitle
			}
			id, err := st.PromoteAudiobook(store.PromoteAudiobookInput{LibraryID: libraryID, Title: title, Path: f.Path})
			if err != nil {
				log.Printf("scanner: promoting audiobook %s: %v", f.ID, err)
				continue
			}
			if err := st.MarkScanFilePromoted(f.ID, id); err != nil {
				log.Printf("scanner: marking scan file %s promoted: %v", f.ID, err)
			}
			continue
		}

		for i, f := range group {
			num := derefOrZero(f.EpisodeNumber)
			if num == 0 {
				num = i + 1
			}
			id, err := st.PromoteChapter(store.PromoteChapterInput{
				LibraryID:     libraryID,
				BookTitle:     bookTitle,
				ChapterNumber: num,
				ChapterTitle:  f.GuessedTitle,
				Path:          f.Path,
			})
			if err != nil {
				log.Printf("scanner: promoting chapter %s: %v", f.ID, err)
				continue
			}
			if err := st.MarkScanFilePromoted(f.ID, id); err != nil {
				log.Printf("scanner: marking scan file %s promoted: %v", f.ID, err)
			}
		}
	}
}

func derefOrZero(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

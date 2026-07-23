package torrent

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// PromoteCompleted turns a finished torrent's video files into browsable
// media_items, exactly like the filesystem scanner promotes scan_files: the
// same filename heuristics decide movie vs. episode, and the same
// PromoteMovie/PromoteEpisode calls create (or reuse) the library rows. A
// torrent with no destination library, or one already promoted, is skipped.
func PromoteCompleted(st *store.Store, t *store.Torrent) {
	if t.LibraryID == nil {
		log.Printf("torrent: %s (%s) completed with no destination library; skipping auto-add", t.ID, t.Name)
		return
	}
	if t.Promoted {
		return
	}

	root := filepath.Join(t.SavePath, t.Name)
	files, err := listVideoFiles(root)
	if err != nil {
		log.Printf("torrent: listing files under %s: %v", root, err)
		return
	}

	for _, path := range files {
		parsed := scanner.ParseFilename(path)
		var promoteErr error
		switch parsed.Kind {
		case "movie":
			_, promoteErr = st.PromoteMovie(store.PromoteMovieInput{
				LibraryID: *t.LibraryID,
				Title:     parsed.Title,
				Year:      yearPtr(parsed.Year),
				Path:      path,
			})
		case "episode":
			_, promoteErr = st.PromoteEpisode(store.PromoteEpisodeInput{
				LibraryID:     *t.LibraryID,
				SeriesTitle:   parsed.Title,
				SeasonNumber:  parsed.SeasonNumber,
				EpisodeNumber: parsed.EpisodeNumber,
				Path:          path,
			})
		default:
			continue
		}
		if promoteErr != nil {
			log.Printf("torrent: promoting %s: %v", path, promoteErr)
		}
	}

	if err := st.MarkTorrentPromoted(t.ID); err != nil {
		log.Printf("torrent: marking %s promoted: %v", t.ID, err)
	}
}

// listVideoFiles returns every video file under root, whether the torrent
// was a single file (root is a file) or a multi-file directory.
func listVideoFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if scanner.IsVideoFile(root) {
			return []string{root}, nil
		}
		return nil, nil
	}

	var out []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && scanner.IsVideoFile(path) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func yearPtr(y int) *int {
	if y == 0 {
		return nil
	}
	return &y
}

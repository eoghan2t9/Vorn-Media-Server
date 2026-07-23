package scanner

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// PromoteDirectory walks root (a single file or a directory) and promotes
// every video file found into media_items under libraryID, using the same
// filename heuristics and PromoteMovie/PromoteEpisode calls as scan_files
// promotion. It's the shared ingestion tail end for any acquisition backend
// (torrent, NZB, ...) whose downloads land on local disk.
func PromoteDirectory(st *store.Store, libraryID, root string) error {
	files, err := listVideoFiles(root)
	if err != nil {
		return err
	}

	for _, path := range files {
		parsed := ParseFilename(path)
		var promoteErr error
		switch parsed.Kind {
		case "movie":
			_, promoteErr = st.PromoteMovie(store.PromoteMovieInput{
				LibraryID: libraryID,
				Title:     parsed.Title,
				Year:      yearPtr(parsed.Year),
				Path:      path,
			})
		case "episode":
			_, promoteErr = st.PromoteEpisode(store.PromoteEpisodeInput{
				LibraryID:     libraryID,
				SeriesTitle:   parsed.Title,
				SeasonNumber:  parsed.SeasonNumber,
				EpisodeNumber: parsed.EpisodeNumber,
				Path:          path,
			})
		default:
			continue
		}
		if promoteErr != nil {
			log.Printf("scanner: promoting %s: %v", path, promoteErr)
		}
	}
	return nil
}

// listVideoFiles returns every video file under root, whether root is a
// single file or a multi-file directory.
func listVideoFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if IsVideoFile(root) {
			return []string{root}, nil
		}
		return nil, nil
	}

	var out []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && IsVideoFile(path) {
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

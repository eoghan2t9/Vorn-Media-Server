package scanner

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
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
		// Probe duration before promoting — same minimum-floor check
		// the scanner and NZB paths use, catching obviously wrong content.
		if parsed.Kind == "movie" || parsed.Kind == "episode" {
			ctx, cancel := context.WithTimeout(context.Background(), scanFileProbeTimeout)
			if err := transcode.VerifyContentDuration(ctx, path, parsed.Kind); err != nil {
				cancel()
				log.Printf("scanner: rejecting %s: %v", path, err)
				continue
			}
			cancel()
		}
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

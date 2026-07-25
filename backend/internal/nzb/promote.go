package nzb

import (
	"log"
	"os"
	"path/filepath"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// PromoteCompleted turns a finished NZB download's video files into
// browsable media_items. A download with no destination library, or one
// already promoted, is skipped. If n resolved via a TorBox-provider Usenet
// server, it has no local files at all -- nzb_files holds a stream URL per
// file instead, and each one is promoted straight from that URL (mirroring
// debrid.PromoteCompleted, no local storage). Otherwise (plain NNTP) it
// falls back to the existing scanner.PromoteDirectory ingestion tail end
// the filesystem scanner and torrent watcher also use.
func PromoteCompleted(st *store.Store, n *store.NZBDownload) {
	if n.LibraryID == nil {
		log.Printf("nzb: %s (%s) completed with no destination library; skipping auto-add", n.ID, n.Name)
		return
	}
	if n.Promoted {
		return
	}

	files, err := st.ListNZBFiles(n.ID)
	if err != nil {
		log.Printf("nzb: listing files for %s: %v", n.ID, err)
		return
	}
	if len(files) > 0 {
		promoteStreamFiles(st, *n.LibraryID, files)
		if err := st.MarkNZBPromoted(n.ID); err != nil {
			log.Printf("nzb: marking %s promoted: %v", n.ID, err)
		}
		return
	}

	root := filepath.Join(n.SavePath, n.Name)
	if err := scanner.PromoteDirectory(st, *n.LibraryID, root); err != nil {
		log.Printf("nzb: promoting files under %s: %v", root, err)
		return
	}
	if err := st.MarkNZBPromoted(n.ID); err != nil {
		log.Printf("nzb: marking %s promoted: %v", n.ID, err)
	}
}

// promoteStreamFiles is PromoteCompleted's TorBox-streamed counterpart to
// scanner.PromoteDirectory: same filename-guess promotion (parse each file's
// title/kind via scanner.ParseFilename), just against nzb_files' stream URLs
// instead of walking a local directory -- mirrors debrid.PromoteCompleted's
// loop exactly.
func promoteStreamFiles(st *store.Store, libraryID string, files []*store.NZBFile) {
	for _, f := range files {
		if !scanner.IsVideoFile(f.Name) {
			continue
		}
		parsed := scanner.ParseFilename(f.Name)
		var promoteErr error
		switch parsed.Kind {
		case "movie":
			_, promoteErr = st.PromoteMovie(store.PromoteMovieInput{
				LibraryID: libraryID,
				Title:     parsed.Title,
				Year:      yearPtr(parsed.Year),
				Path:      f.StreamURL,
			})
		case "episode":
			_, promoteErr = st.PromoteEpisode(store.PromoteEpisodeInput{
				LibraryID:     libraryID,
				SeriesTitle:   parsed.Title,
				SeasonNumber:  parsed.SeasonNumber,
				EpisodeNumber: parsed.EpisodeNumber,
				Path:          f.StreamURL,
			})
		default:
			continue
		}
		if promoteErr != nil {
			log.Printf("nzb: promoting %s: %v", f.Name, promoteErr)
		}
	}
}

func yearPtr(y int) *int {
	if y == 0 {
		return nil
	}
	return &y
}

// PromoteToExistingItem fulfils a specific placeholder media_item from a
// completed on-demand NZB download -- the counterpart to
// debrid.PromoteToExistingItem. If rec resolved via a TorBox-provider
// Usenet server, nzb_files holds a stream URL per file and mediaItem.Path
// is pointed straight at the largest one (no local storage, mirroring
// debrid's own promotion). Otherwise (plain NNTP, which has no equivalent
// direct-URL concept) it falls back to walking rec.SavePath/rec.Name on
// disk and picking the largest video file found there.
func PromoteToExistingItem(st *store.Store, mediaItem *store.MediaItem, rec *store.NZBDownload) {
	files, err := st.ListNZBFiles(rec.ID)
	if err != nil {
		log.Printf("nzb: listing files for %s: %v", rec.ID, err)
		return
	}
	if len(files) > 0 {
		best := largestVideoNZBFile(files)
		if best == nil {
			if err := st.SetMediaItemAcquisitionError(mediaItem.ID, "resolved NZB had no video file"); err != nil {
				log.Printf("nzb: setting acquisition error on %s: %v", mediaItem.ID, err)
			}
			return
		}
		if err := st.SetMediaItemPath(mediaItem.ID, best.StreamURL, rec.Name); err != nil {
			log.Printf("nzb: setting path on %s: %v", mediaItem.ID, err)
			return
		}
		if err := st.MarkNZBPromoted(rec.ID); err != nil {
			log.Printf("nzb: marking %s promoted: %v", rec.ID, err)
		}
		return
	}

	best, bestSize := "", int64(-1)
	outDir := filepath.Join(rec.SavePath, rec.Name)
	entries, err := os.ReadDir(outDir)
	if err != nil {
		log.Printf("nzb: reading %s: %v", outDir, err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !scanner.IsVideoFile(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			best, bestSize = filepath.Join(outDir, e.Name()), info.Size()
		}
	}

	if best == "" {
		if err := st.SetMediaItemAcquisitionError(mediaItem.ID, "resolved NZB had no video file"); err != nil {
			log.Printf("nzb: setting acquisition error on %s: %v", mediaItem.ID, err)
		}
		return
	}

	if err := st.SetMediaItemPath(mediaItem.ID, best, rec.Name); err != nil {
		log.Printf("nzb: setting path on %s: %v", mediaItem.ID, err)
		return
	}
	if err := st.MarkNZBPromoted(rec.ID); err != nil {
		log.Printf("nzb: marking %s promoted: %v", rec.ID, err)
	}
}

func largestVideoNZBFile(files []*store.NZBFile) *store.NZBFile {
	var best *store.NZBFile
	for _, f := range files {
		if !scanner.IsVideoFile(f.Name) {
			continue
		}
		if best == nil || f.SizeBytes > best.SizeBytes {
			best = f
		}
	}
	return best
}

// PromoteSeasonPackToExistingItems mirrors
// debrid.PromoteSeasonPackToExistingItems: if rec resolved via a
// TorBox-provider Usenet server, nzb_files holds a stream URL per file and
// each episode's path is pointed straight at its matching one (no local
// storage). Otherwise (plain NNTP) rec's download directory holds the whole
// season on disk instead, and this falls back to walking it. Either way,
// each file is matched to its episode by parsing season/episode numbers out
// of its own filename (scanner.ParseFilename, the same parser scan-promoted
// episodes use) and looked up among seasonItem's episode children --
// unmatched files (extras, samples) are skipped, and the larger file wins if
// two match the same episode.
func PromoteSeasonPackToExistingItems(st *store.Store, seasonItem *store.MediaItem, rec *store.NZBDownload) {
	episodes, err := st.ListChildren(seasonItem.ID)
	if err != nil {
		log.Printf("nzb: listing episodes for season %s: %v", seasonItem.ID, err)
		return
	}
	byEpisodeNumber := make(map[int]*store.MediaItem, len(episodes))
	for _, ep := range episodes {
		if ep.EpisodeNumber != nil {
			byEpisodeNumber[*ep.EpisodeNumber] = ep
		}
	}

	files, err := st.ListNZBFiles(rec.ID)
	if err != nil {
		log.Printf("nzb: listing files for %s: %v", rec.ID, err)
		return
	}

	type sized struct {
		path string
		size int64
	}
	bestPerEpisode := make(map[int]sized)

	if len(files) > 0 {
		for _, f := range files {
			if !scanner.IsVideoFile(f.Name) {
				continue
			}
			parsed := scanner.ParseFilename(f.Name)
			if parsed.Kind != "episode" || parsed.EpisodeNumber == 0 {
				continue
			}
			if cur, ok := bestPerEpisode[parsed.EpisodeNumber]; !ok || f.SizeBytes > cur.size {
				bestPerEpisode[parsed.EpisodeNumber] = sized{path: f.StreamURL, size: f.SizeBytes}
			}
		}
	} else {
		outDir := filepath.Join(rec.SavePath, rec.Name)
		entries, err := os.ReadDir(outDir)
		if err != nil {
			log.Printf("nzb: reading %s: %v", outDir, err)
			return
		}
		for _, e := range entries {
			if e.IsDir() || !scanner.IsVideoFile(e.Name()) {
				continue
			}
			parsed := scanner.ParseFilename(e.Name())
			if parsed.Kind != "episode" || parsed.EpisodeNumber == 0 {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if cur, ok := bestPerEpisode[parsed.EpisodeNumber]; !ok || info.Size() > cur.size {
				bestPerEpisode[parsed.EpisodeNumber] = sized{path: filepath.Join(outDir, e.Name()), size: info.Size()}
			}
		}
	}

	if len(bestPerEpisode) == 0 {
		// Nothing in the release could be attributed to any episode --
		// treated exactly like PromoteToExistingItem's own no-video-file
		// case, so AcquireSeasonPack's fencing-aware fallback to per-episode
		// acquisition sees this season attempt as failed.
		if err := st.SetMediaItemAcquisitionError(seasonItem.ID, "resolved season pack had no recognizable episode files"); err != nil {
			log.Printf("nzb: setting acquisition error on %s: %v", seasonItem.ID, err)
		}
		return
	}

	promoted := 0
	for epNum, f := range bestPerEpisode {
		ep, ok := byEpisodeNumber[epNum]
		if !ok {
			continue // pack contains an episode not in Vorn's synced tree (e.g. a special) -- ignore it
		}
		if err := st.SetMediaItemPath(ep.ID, f.path, rec.Name); err != nil {
			log.Printf("nzb: setting path on episode %s: %v", ep.ID, err)
			continue
		}
		promoted++
	}

	// The season row itself has no path of its own (it's a container, same
	// as any scan-promoted season) -- mark it 'owned' purely so
	// MonitorScheduler's placeholder sweep and any future re-run of
	// AcquireSeasonPack treat this season as settled rather than retrying it.
	if promoted > 0 {
		if err := st.SetMediaItemAcquisitionStatus(seasonItem.ID, "owned"); err != nil {
			log.Printf("nzb: marking season %s owned: %v", seasonItem.ID, err)
		}
	}
	if err := st.MarkNZBPromoted(rec.ID); err != nil {
		log.Printf("nzb: marking %s promoted: %v", rec.ID, err)
	}
}

// AuthorizedMediaItem loads rec.MediaItemID and returns it only if rec is
// still that media_item's recorded active_nzb_download_id -- nil (with no
// error) means a later resolve attempt has since taken over and rec's
// outcome must be discarded without promoting anything. Mirrors
// debrid.AuthorizedMediaItem exactly.
func AuthorizedMediaItem(st *store.Store, rec *store.NZBDownload) (*store.MediaItem, error) {
	mediaItem, err := st.GetMediaItem(*rec.MediaItemID)
	if err != nil {
		return nil, err
	}
	if mediaItem.ActiveNZBDownloadID == nil || *mediaItem.ActiveNZBDownloadID != rec.ID {
		return nil, nil
	}
	return mediaItem, nil
}

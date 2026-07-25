package nzb

import (
	"log"
	"os"
	"path/filepath"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// PromoteCompleted turns a finished NZB download's video files into
// browsable media_items via scanner.PromoteDirectory, the same ingestion
// tail end the filesystem scanner and torrent watcher use. A download with
// no destination library, or one already promoted, is skipped.
func PromoteCompleted(st *store.Store, n *store.NZBDownload) {
	if n.LibraryID == nil {
		log.Printf("nzb: %s (%s) completed with no destination library; skipping auto-add", n.ID, n.Name)
		return
	}
	if n.Promoted {
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

// PromoteToExistingItem fulfils a specific placeholder media_item from a
// completed on-demand NZB download -- the counterpart to
// debrid.PromoteToExistingItem. Unlike debrid, there's no stream URL: both
// download paths this package supports (plain NNTP and TorBox's Usenet
// caching) always write real files under rec.SavePath/rec.Name, so this
// just walks that directory and points mediaItem.Path at the largest video
// file found in it, the same "largest file wins" tie-break debrid's own
// promotion uses.
func PromoteToExistingItem(st *store.Store, mediaItem *store.MediaItem, rec *store.NZBDownload) {
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

// PromoteSeasonPackToExistingItems mirrors
// debrid.PromoteSeasonPackToExistingItems: rec's download directory holds a
// whole season, so each file is matched to its episode by parsing
// season/episode numbers out of its own filename (scanner.ParseFilename,
// the same parser scan-promoted episodes use) and looked up among
// seasonItem's episode children -- unmatched files (extras, samples) are
// skipped, and the larger file wins if two match the same episode.
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

	outDir := filepath.Join(rec.SavePath, rec.Name)
	entries, err := os.ReadDir(outDir)
	if err != nil {
		log.Printf("nzb: reading %s: %v", outDir, err)
		return
	}

	type sized struct {
		path string
		size int64
	}
	bestPerEpisode := make(map[int]sized)
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

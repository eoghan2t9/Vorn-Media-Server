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

// PromoteToExistingItem fulfils a specific placeholder movie/episode
// media_item (created by on-demand acquisition, see the acquisition
// package) rather than filename-guessing a new one the way PromoteCompleted
// does for manual/admin-added links -- mediaItem is already known, so this
// just needs to pick a file and point that exact row at it.
//
// item may be one of several resolve attempts the acquisition package
// tried against mediaItem (retry candidates, a quality-upgrade re-check) --
// some of those attempts can still be running in the background well after
// their caller gave up on them (see acquisition.Service.waitForOutcome).
// The caller (debrid.Service's onComplete, via AuthorizedMediaItem) has
// already confirmed item is still mediaItem's active_debrid_item_id before
// calling this, so a late-finishing abandoned attempt never reaches here at
// all -- it can't silently overwrite a newer result or resurrect an item
// already marked failed by a subsequent attempt.
func PromoteToExistingItem(st *store.Store, mediaItem *store.MediaItem, item *store.DebridItem) {
	files, err := st.ListDebridFiles(item.ID)
	if err != nil {
		log.Printf("debrid: listing files for %s: %v", item.ID, err)
		return
	}

	best := largestVideoFile(files)
	if best == nil {
		if err := st.SetMediaItemAcquisitionError(mediaItem.ID, "resolved release had no video file"); err != nil {
			log.Printf("debrid: setting acquisition error on %s: %v", mediaItem.ID, err)
		}
		return
	}

	if err := st.SetMediaItemPath(mediaItem.ID, best.StreamURL, item.Name); err != nil {
		log.Printf("debrid: setting path on %s: %v", mediaItem.ID, err)
		return
	}
	if err := st.MarkDebridItemPromoted(item.ID); err != nil {
		log.Printf("debrid: marking %s promoted: %v", item.ID, err)
	}
}

// PromoteSeasonPackToExistingItems fulfils a whole season in one go: unlike
// PromoteToExistingItem, seasonItem.MediaItemID (via item) points at the
// SEASON row, not a single episode, because one season-pack release covers
// many episodes at once (see acquisition.Service.AcquireSeasonPack). Each
// resolved video file is matched to its episode by parsing season/episode
// numbers out of its own filename with scanner.ParseFilename (the same
// parser scan-promoted episodes already use) and looked up among
// seasonItem's episode children; unmatched files (extras, samples,
// non-standard names) are skipped, and if two files match the same episode
// (a theatrical + extended cut, say) the larger one wins, mirroring
// PromoteToExistingItem's own single-file tie-break.
func PromoteSeasonPackToExistingItems(st *store.Store, seasonItem *store.MediaItem, item *store.DebridItem) {
	episodes, err := st.ListChildren(seasonItem.ID)
	if err != nil {
		log.Printf("debrid: listing episodes for season %s: %v", seasonItem.ID, err)
		return
	}
	byEpisodeNumber := make(map[int]*store.MediaItem, len(episodes))
	for _, ep := range episodes {
		if ep.EpisodeNumber != nil {
			byEpisodeNumber[*ep.EpisodeNumber] = ep
		}
	}

	files, err := st.ListDebridFiles(item.ID)
	if err != nil {
		log.Printf("debrid: listing files for %s: %v", item.ID, err)
		return
	}

	bestPerEpisode := make(map[int]*store.DebridFile)
	for _, f := range files {
		if !scanner.IsVideoFile(f.Name) {
			continue
		}
		parsed := scanner.ParseFilename(f.Name)
		if parsed.Kind != "episode" || parsed.EpisodeNumber == 0 {
			continue
		}
		if cur, ok := bestPerEpisode[parsed.EpisodeNumber]; !ok || f.SizeBytes > cur.SizeBytes {
			bestPerEpisode[parsed.EpisodeNumber] = f
		}
	}

	if len(bestPerEpisode) == 0 {
		// Nothing in the release could be attributed to any episode --
		// treated exactly like PromoteToExistingItem's own no-video-file
		// case, so AcquireSeasonPack's fencing-aware fallback to per-episode
		// acquisition sees this season attempt as failed.
		if err := st.SetMediaItemAcquisitionError(seasonItem.ID, "resolved season pack had no recognizable episode files"); err != nil {
			log.Printf("debrid: setting acquisition error on %s: %v", seasonItem.ID, err)
		}
		return
	}

	promoted := 0
	for epNum, f := range bestPerEpisode {
		ep, ok := byEpisodeNumber[epNum]
		if !ok {
			continue // pack contains an episode not in Vorn's synced tree (e.g. a special) -- ignore it
		}
		if err := st.SetMediaItemPath(ep.ID, f.StreamURL, item.Name); err != nil {
			log.Printf("debrid: setting path on episode %s: %v", ep.ID, err)
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
			log.Printf("debrid: marking season %s owned: %v", seasonItem.ID, err)
		}
	}
	if err := st.MarkDebridItemPromoted(item.ID); err != nil {
		log.Printf("debrid: marking %s promoted: %v", item.ID, err)
	}
}

func largestVideoFile(files []*store.DebridFile) *store.DebridFile {
	var best *store.DebridFile
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

// AuthorizedMediaItem loads item.MediaItemID and returns it only if item is
// still that media_item's recorded active_debrid_item_id -- nil (with no
// error) means a later resolve attempt has since taken over and item's
// outcome must be discarded without promoting anything.
func AuthorizedMediaItem(st *store.Store, item *store.DebridItem) (*store.MediaItem, error) {
	mediaItem, err := st.GetMediaItem(*item.MediaItemID)
	if err != nil {
		return nil, err
	}
	if mediaItem.ActiveDebridItemID == nil || *mediaItem.ActiveDebridItemID != item.ID {
		return nil, nil
	}
	return mediaItem, nil
}

func yearPtr(y int) *int {
	if y == 0 {
		return nil
	}
	return &y
}

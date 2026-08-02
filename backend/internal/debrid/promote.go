package debrid

import (
	"context"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
)

// verifyProbeTimeout bounds how long a content-verification probe (see
// transcode.VerifyRuntime) is allowed to take against a resolved stream
// URL -- generous relative to the ~2s a real probe against a provider CDN
// takes in practice, without risking hanging promotion indefinitely on a
// slow/dead link.
const verifyProbeTimeout = 25 * time.Second

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
// item may be one of several candidates acquisition.Service raced
// concurrently against mediaItem -- returns whether item specifically ended
// up being the one used, so the caller (debrid.Service's onComplete) knows
// whether to delete item from the provider instead of leaking it. On any
// rejection (no video file, failed verification, lost the claim to a
// sibling candidate that got there first) this deliberately does NOT touch
// mediaItem at all -- a sibling might still be resolving, and only
// acquisition.Service, which knows how many candidates it launched, is in a
// position to decide the whole attempt has failed.
func PromoteToExistingItem(st *store.Store, mediaItem *store.MediaItem, item *store.DebridItem) bool {
	files, err := st.ListDebridFiles(item.ID)
	if err != nil {
		log.Printf("debrid: listing files for %s: %v", item.ID, err)
		return false
	}

	candidates := topVideoFiles(files, 2)
	if len(candidates) == 0 {
		return false
	}

	var best *store.DebridFile
	for _, candidate := range candidates {
		if mediaItem.RuntimeMinutes != nil {
			ctx, cancel := context.WithTimeout(context.Background(), verifyProbeTimeout)
			verifyErr := transcode.VerifyRuntime(ctx, candidate.StreamURL, *mediaItem.RuntimeMinutes)
			cancel()
			if verifyErr != nil {
				log.Printf("debrid: content verification failed for %s (file %s): %v — trying next candidate", mediaItem.ID, candidate.Name, verifyErr)
				continue
			}
		}
		best = candidate
		break
	}
	if best == nil {
		log.Printf("debrid: content verification failed for all %d video files for %s", len(candidates), mediaItem.ID)
		return false
	}

	claimed, err := st.ClaimMediaItemForDebridItem(mediaItem.ID, item.ID)
	if err != nil {
		log.Printf("debrid: claiming %s for %s: %v", mediaItem.ID, item.ID, err)
		return false
	}
	if !claimed {
		return false
	}

	if err := st.SetMediaItemPath(mediaItem.ID, best.StreamURL, item.Name); err != nil {
		log.Printf("debrid: setting path on %s: %v", mediaItem.ID, err)
		return true // claimed and the stream is real -- don't delete it just because this write failed
	}
	if err := st.MarkDebridItemPromoted(item.ID); err != nil {
		log.Printf("debrid: marking %s promoted: %v", item.ID, err)
	}
	return true
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
// PromoteToExistingItem's own single-file tie-break. Returns whether item
// ended up promoting at least one episode -- see PromoteToExistingItem for
// why the caller needs this and why a rejection never touches seasonItem.
func PromoteSeasonPackToExistingItems(st *store.Store, seasonItem *store.MediaItem, item *store.DebridItem) bool {
	episodes, err := st.ListChildren(seasonItem.ID)
	if err != nil {
		log.Printf("debrid: listing episodes for season %s: %v", seasonItem.ID, err)
		return false
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
		return false
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

	// Claimed (and therefore verified/matched) per episode independently --
	// racing season-pack candidates can legitimately split a season, each
	// winning whichever episodes it resolves first, rather than all-or-
	// nothing against the season as a whole.
	promoted := 0
	for epNum, f := range bestPerEpisode {
		ep, ok := byEpisodeNumber[epNum]
		if !ok {
			continue // pack contains an episode not in Vorn's synced tree (e.g. a special) -- ignore it
		}
		if ep.RuntimeMinutes != nil {
			ctx, cancel := context.WithTimeout(context.Background(), verifyProbeTimeout)
			verifyErr := transcode.VerifyRuntime(ctx, f.StreamURL, *ep.RuntimeMinutes)
			cancel()
			if verifyErr != nil {
				log.Printf("debrid: content verification failed for episode %s: %v", ep.ID, verifyErr)
				continue // treated like an unmatched file -- skip this episode, not the whole pack
			}
		}
		claimed, err := st.ClaimMediaItemForDebridItem(ep.ID, item.ID)
		if err != nil {
			log.Printf("debrid: claiming episode %s for %s: %v", ep.ID, item.ID, err)
			continue
		}
		if !claimed {
			continue // a sibling racing candidate already won this episode
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
		if err := st.MarkDebridItemPromoted(item.ID); err != nil {
			log.Printf("debrid: marking %s promoted: %v", item.ID, err)
		}
	}
	return promoted > 0
}

// largestVideoFile returns the single largest video file in files, or nil
// if there are none. Used by callers that want exactly one best pick.
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

// topVideoFiles returns the N largest video files sorted descending by
// size, so a caller failing content verification against the top pick can
// fall back to the next one instead of wasting the entire candidate.
func topVideoFiles(files []*store.DebridFile, n int) []*store.DebridFile {
	var video []*store.DebridFile
	for _, f := range files {
		if scanner.IsVideoFile(f.Name) {
			video = append(video, f)
		}
	}
	// Sort descending by size (simple insertion sort — n ≤ 3 in practice).
	for i := 1; i < len(video); i++ {
		for j := i; j > 0 && video[j].SizeBytes > video[j-1].SizeBytes; j-- {
			video[j], video[j-1] = video[j-1], video[j]
		}
	}
	if len(video) > n {
		return video[:n]
	}
	return video
}

func yearPtr(y int) *int {
	if y == 0 {
		return nil
	}
	return &y
}

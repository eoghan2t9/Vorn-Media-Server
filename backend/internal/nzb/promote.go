package nzb

import (
	"context"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
)

// verifyProbeTimeout bounds how long a content-verification probe (see
// transcode.VerifyRuntime) is allowed to take against a resolved stream URL
// -- generous relative to the couple of seconds a real probe takes in
// practice, without risking hanging promotion indefinitely on a slow/dead
// link.
const verifyProbeTimeout = 25 * time.Second

// verifyRuntime is a small wrapper so both PromoteToExistingItem and
// PromoteSeasonPackToExistingItems can check a resolved stream URL against a
// media_item's expected TMDb runtime with one line, logging and returning
// false on any failure. Always probes the file — even when TMDb data isn't
// available yet, the minimum-duration floor (10 min movies, 2 min episodes)
// catches obviously wrong content.
func verifyRuntime(logPrefix, path string, item *store.MediaItem) bool {
	ctx, cancel := context.WithTimeout(context.Background(), verifyProbeTimeout)
	defer cancel()

	// Always check minimum duration — catches obviously wrong content
	// even when TMDb data hasn't been backfilled yet.
	if err := transcode.VerifyContentDuration(ctx, path, item.Kind); err != nil {
		log.Printf("nzb: content verification failed for %s: %v", logPrefix, err)
		return false
	}

	if item.RuntimeMinutes != nil && *item.RuntimeMinutes > 0 {
		if err := transcode.VerifyRuntime(ctx, path, *item.RuntimeMinutes); err != nil {
			log.Printf("nzb: content verification failed for %s: %v", logPrefix, err)
			return false
		}
	}
	return true
}

// PromoteCompleted turns a finished NZB download's video files into
// browsable media_items. A download with no destination library, or one
// already promoted, is skipped. nzb_files holds a stream URL per file
// (TorBox never puts anything on local disk), so each is promoted straight
// from that URL (mirroring debrid.PromoteCompleted), even if the list comes
// back empty (a real TorBox edge case -- treated as "no video file").
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
	promoteStreamFiles(st, *n.LibraryID, files)
	if err := st.MarkNZBPromoted(n.ID); err != nil {
		log.Printf("nzb: marking %s promoted: %v", n.ID, err)
	}
}

// promoteStreamFiles is PromoteCompleted's filename-guess promotion step
// (parse each file's title/kind via scanner.ParseFilename) against
// nzb_files' stream URLs -- mirrors debrid.PromoteCompleted's loop exactly.
func promoteStreamFiles(st *store.Store, libraryID string, files []*store.NZBFile) {
	for _, f := range files {
		if !scanner.IsVideoFile(f.Name) {
			continue
		}
		// Skip hash-named files — TorBox renames NZB downloads to
		// random hashes, and ParseFilename can't distinguish those
		// from real titles. Real filenames (like xXx.2002.1080p.mkv)
		// always have word separators that IsProbableHash checks for.
		if scanner.IsProbableHash(f.Name) {
			continue
		}
		parsed := scanner.ParseFilename(f.Name)
		path := bestPathForPromotion(f)

		// Probe every file before promoting — library-level promotion
		// has no TMDb data to compare against, but a minimum-duration
		// check catches obviously wrong content (porno, samples, etc.)
		// even when the filename looks legitimate.
		if err := verifyContentDuration(path, parsed.Kind); err != nil {
			log.Printf("nzb: rejecting %s: %v", f.Name, err)
			continue
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
			log.Printf("nzb: promoting %s: %v", f.Name, promoteErr)
		}
	}
}

// verifyContentDuration probes path and rejects it if the file is too
// short for its parsed kind. Factored out of promoteStreamFiles so it can
// be called from a single probe + reuse its result across both the minimum
// check and the caller's own promotion logic without probing twice.
func verifyContentDuration(path, kind string) error {
	ctx, cancel := context.WithTimeout(context.Background(), verifyProbeTimeout)
	defer cancel()
	return transcode.VerifyContentDuration(ctx, path, kind)
}

func yearPtr(y int) *int {
	if y == 0 {
		return nil
	}
	return &y
}

// bestPathForPromotion returns the stable WebDAV URL if one was matched
// (by size at download time — see Service.matchWebDAVURLs), otherwise
// falls back to the expiring CDN stream URL.
func bestPathForPromotion(f *store.NZBFile) string {
	if f.WebDAVURL != "" {
		return f.WebDAVURL
	}
	return f.StreamURL
}

// PromoteToExistingItem fulfils a specific placeholder media_item from a
// completed on-demand NZB download -- the counterpart to
// debrid.PromoteToExistingItem. nzb_files holds a stream URL per file, and
// mediaItem.Path is pointed straight at the largest video one (no local
// storage, mirroring debrid's own promotion) -- an empty file list is a
// genuine "no video file" outcome. Returns whether rec specifically ended up
// being the one used, so the caller (nzb.Service's onComplete) knows
// whether to delete rec from TorBox instead of leaking it. On any rejection
// (no video file, failed verification, lost the claim to a sibling
// candidate that got there first) this deliberately does NOT touch
// mediaItem at all -- a sibling might still be resolving, and only
// acquisition.Service, which knows how many candidates it launched, is in a
// position to decide the whole attempt has failed.
func PromoteToExistingItem(st *store.Store, mediaItem *store.MediaItem, rec *store.NZBDownload) bool {
	files, err := st.ListNZBFiles(rec.ID)
	if err != nil {
		log.Printf("nzb: listing files for %s: %v", rec.ID, err)
		return false
	}
	candidates := topVideoNZBFiles(files, 2)
	if len(candidates) == 0 {
		return false
	}
	// Prefer the stable WebDAV URL (matched by size after TorBox caching)
	// over the expiring CDN stream URL — for both content verification and
	// the final media_item Path.
	var best *store.NZBFile
	var verifyPath string
	for _, candidate := range candidates {
		vp := bestPathForPromotion(candidate)
		if verifyRuntime(mediaItem.ID, vp, mediaItem) {
			best = candidate
			verifyPath = vp
			break
		}
		log.Printf("nzb: content verification failed for %s (file %s) — trying next candidate", mediaItem.ID, candidate.Name)
	}
	if best == nil {
		log.Printf("nzb: content verification failed for all %d video files for %s", len(candidates), mediaItem.ID)
		return false
	}
	claimed, err := st.ClaimMediaItemForNZBDownload(mediaItem.ID, rec.ID)
	if err != nil {
		log.Printf("nzb: claiming %s for %s: %v", mediaItem.ID, rec.ID, err)
		return false
	}
	if !claimed {
		return false
	}
	if err := st.SetMediaItemPath(mediaItem.ID, verifyPath, rec.Name); err != nil {
		log.Printf("nzb: setting path on %s: %v", mediaItem.ID, err)
		return true // claimed and the stream is real -- don't delete it just because this write failed
	}
	if err := st.MarkNZBPromoted(rec.ID); err != nil {
		log.Printf("nzb: marking %s promoted: %v", rec.ID, err)
	}
	return true
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

// topVideoNZBFiles returns the N largest video files sorted descending by
// size, so a caller failing content verification against the top pick can
// fall back to the next one instead of wasting the entire candidate.
func topVideoNZBFiles(files []*store.NZBFile, n int) []*store.NZBFile {
	var video []*store.NZBFile
	for _, f := range files {
		if scanner.IsVideoFile(f.Name) {
			video = append(video, f)
		}
	}
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

// PromoteSeasonPackToExistingItems mirrors
// debrid.PromoteSeasonPackToExistingItems: nzb_files holds a stream URL per
// file, and each episode's path is pointed straight at its matching one (no
// local storage). Each file is matched to its episode by parsing
// season/episode numbers out of its own filename (scanner.ParseFilename,
// the same parser scan-promoted episodes use) and looked up among
// seasonItem's episode children -- unmatched files (extras, samples) are
// skipped, and the larger file wins if two match the same episode. Returns
// whether rec ended up promoting at least one episode -- see
// PromoteToExistingItem for why the caller needs this and why a rejection
// never touches seasonItem.
func PromoteSeasonPackToExistingItems(st *store.Store, seasonItem *store.MediaItem, rec *store.NZBDownload) bool {
	episodes, err := st.ListChildren(seasonItem.ID)
	if err != nil {
		log.Printf("nzb: listing episodes for season %s: %v", seasonItem.ID, err)
		return false
	}
	byEpisodeNumber := make(map[int]*store.MediaItem, len(episodes))
	for _, ep := range episodes {
		if ep.EpisodeNumber != nil {
			byEpisodeNumber[*ep.EpisodeNumber] = ep
		}
	}

	type sized struct {
		path string
		size int64
	}
	bestPerEpisode := make(map[int]sized)

	files, err := st.ListNZBFiles(rec.ID)
	if err != nil {
		log.Printf("nzb: listing files for %s: %v", rec.ID, err)
		return false
	}
	for _, f := range files {
		if !scanner.IsVideoFile(f.Name) {
			continue
		}
		parsed := scanner.ParseFilename(f.Name)
		if parsed.Kind != "episode" || parsed.EpisodeNumber == 0 {
			continue
		}
		if cur, ok := bestPerEpisode[parsed.EpisodeNumber]; !ok || f.SizeBytes > cur.size {
			bestPerEpisode[parsed.EpisodeNumber] = sized{path: bestPathForPromotion(f), size: f.SizeBytes}
		}
	}

	// Claimed per episode independently -- racing season-pack candidates
	// can legitimately split a season, each winning whichever episodes it
	// resolves first, rather than all-or-nothing against the season as a
	// whole.
	promoted := 0
	for epNum, f := range bestPerEpisode {
		ep, ok := byEpisodeNumber[epNum]
		if !ok {
			continue // pack contains an episode not in Vorn's synced tree (e.g. a special) -- ignore it
		}
		if !verifyRuntime(ep.ID, f.path, ep) {
			continue // treated like an unmatched file -- skip this episode, not the whole pack
		}
		claimed, err := st.ClaimMediaItemForNZBDownload(ep.ID, rec.ID)
		if err != nil {
			log.Printf("nzb: claiming episode %s for %s: %v", ep.ID, rec.ID, err)
			continue
		}
		if !claimed {
			continue // a sibling racing candidate already won this episode
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
		if err := st.MarkNZBPromoted(rec.ID); err != nil {
			log.Printf("nzb: marking %s promoted: %v", rec.ID, err)
		}
	}
	return promoted > 0
}

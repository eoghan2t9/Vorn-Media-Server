package nzb

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const providerDeleteTimeout = 30 * time.Second
const defaultBackgroundSyncInterval = 30 * time.Second

// NZBDownloadEvent is published to SSE subscribers whenever a download
// completes (via catchUpDownload or importOrphanedDownload). The event is
// minimal — the client fetches full state via the list endpoint, same as
// the polling interval it replaces.
type NZBDownloadEvent struct{}

// Service resolves NZB releases via a configured TorBox account: parsing
// the index, handing it to TorBox for server-side caching/repair, then
// recording the direct stream URLs TorBox returns and promoting the result
// into the library on completion. No article data ever passes through
// Vorn itself.
type Service struct {
	store *store.Store
	// onComplete returns whether rec's files actually ended up being used
	// (promoted into a media item, or a manual admin add) -- run() deletes
	// rec from TorBox when it returns false, since that means nothing will
	// ever use the quota/storage it's holding.
	onComplete func(*store.NZBDownload) bool
	// torboxClient is constructed once and reused for every download/
	// account-test, sharing torboxLimiter with debrid.Service's own TorBox
	// client and torrent.Service's indexer search -- a fresh
	// debrid.NewTorBoxClient() per call (or an unshared limiter) would give
	// every single attempt its own independent "fresh" 300/min budget
	// instead of one real, shared cap across every TorBox interaction this
	// process makes.
	torboxClient *debrid.TorBoxClient
	// syncCancel stops the continuous background poller goroutine. Guarded
	// by syncMu since StartBackgroundSync and Close may be called from
	// different goroutines.
	syncCancel context.CancelFunc
	syncMu     sync.Mutex
	// syncRunning prevents overlapping syncFromTorBox sweeps -- if the
	// previous sweep is still running when the ticker fires, the new tick
	// is skipped.
	syncRunning sync.Mutex
	// subscribers is the SSE broadcast list: every channel receives a
	// ping when a download completes. Guarded by subMu.
	subscribers map[chan NZBDownloadEvent]struct{}
	subMu       sync.Mutex
	// instantAvailCache avoids redundant TorBox cache-check API calls for
	// the same NZB URL hash within a short window -- keyed on the MD5 hash
	// TorBox uses for its own cache check. Guarded by iaMu; nil value means
	// "checked and not cached" (explicit negative cache).
	instantAvailCache map[string]*instantAvailEntry
	iaMu              sync.Mutex
	// failureCache skips re-submitting NZBs whose download URLs recently
	// failed (bad NZB file, server error, permanent failure) -- avoids
	// wasting TorBox quota on known-bad content. Guarded by fcMu.
	failureCache map[string]time.Time
	fcMu         sync.Mutex
}

type instantAvailEntry struct {
	info      *debrid.NZBInstantAvailInfo
	expiresAt time.Time
}

const (
	instantAvailTTL  = 5 * time.Minute
	failureCacheTTL  = 30 * time.Minute
	failureCacheSize = 500 // max entries before oldest are evicted
)

// NewService takes torboxLimiter (see debrid.Service.TorBoxLimiter) rather
// than constructing its own, so NZB's TorBox usenet-caching client shares
// the exact same rate budget as debrid.Service's TorBox debrid-resolve
// client and torrent.Service's TorBox indexer search.
func NewService(st *store.Store, torboxLimiter *debrid.Limiter) *Service {
	svc := &Service{
		store:             st,
		torboxClient:      debrid.NewTorBoxClient(torboxLimiter),
		subscribers:       make(map[chan NZBDownloadEvent]struct{}),
		instantAvailCache: make(map[string]*instantAvailEntry),
		failureCache:      make(map[string]time.Time),
	}
	svc.onComplete = func(n *store.NZBDownload) bool {
		if n.MediaItemID == nil {
			// Manual/admin-added download -- never auto-delete from TorBox
			// even if filename-guess promotion finds nothing to promote,
			// since the admin explicitly asked for this one.
			PromoteCompleted(st, n)
			return true
		}
		mediaItem, err := st.GetMediaItem(*n.MediaItemID)
		if err != nil {
			log.Printf("nzb: loading media item for %s: %v", n.ID, err)
			return false
		}
		if mediaItem.Kind == "season" {
			return PromoteSeasonPackToExistingItems(st, mediaItem, n)
		}
		return PromoteToExistingItem(st, mediaItem, n)
	}
	return svc
}

// AddNZB parses a .nzb file's bytes and starts downloading it in the
// background against whichever configured Usenet server is enabled --
// the manual, admin-driven flow (Admin > NZB), which always
// filename-guess-promotes into libraryID at large (see PromoteCompleted).
// ctx bounds the resolve itself, same as debrid.Service.AddLink -- manual/
// admin callers pass context.Background().
func (svc *Service) AddNZB(ctx context.Context, data []byte, libraryID *string) (*store.NZBDownload, error) {
	return svc.addNZB(ctx, data, libraryID, nil)
}

// AddNZBForItem is AddNZB's on-demand-acquisition counterpart (the NZB
// analog of debrid.Service.AddLink): it targets a specific existing
// placeholder media_item rather than filename-guessing into a library.
// acquisition.Service passes a context it cancels once it stops caring
// about this attempt (a rival candidate already won, or it's giving up),
// so a losing racer stops within one HTTP round-trip instead of running to
// completion pointlessly.
func (svc *Service) AddNZBForItem(ctx context.Context, data []byte, libraryID, mediaItemID string) (*store.NZBDownload, error) {
	return svc.addNZB(ctx, data, &libraryID, &mediaItemID)
}

func (svc *Service) addNZB(ctx context.Context, data []byte, libraryID, mediaItemID *string) (*store.NZBDownload, error) {
	doc, err := Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("nzb: parsing nzb file: %w", err)
	}
	if len(doc.Files) == 0 {
		return nil, fmt.Errorf("nzb: nzb file has no <file> entries")
	}

	name := doc.Title()
	if name == "" {
		name = SubjectFilename(doc.Files[0].Subject)
	}

	rec, err := svc.store.CreateNZBDownload(store.CreateNZBDownloadInput{
		LibraryID:   libraryID,
		MediaItemID: mediaItemID,
		Name:        name,
	})
	if err != nil {
		return nil, err
	}

	go svc.run(ctx, rec, data)
	return rec, nil
}

func (svc *Service) run(ctx context.Context, rec *store.NZBDownload, data []byte) {
	server, err := svc.pickServer()
	if err != nil {
		svc.finish(rec, err)
		return
	}
	svc.runTorBox(ctx, rec, data, server)
}

// TestTorBoxAccount verifies a TorBox API key by fetching account info,
// without requiring it to be saved as a usenet server first.
func (svc *Service) TestTorBoxAccount(ctx context.Context, apiKey string) error {
	_, err := svc.torboxClient.AccountInfo(ctx, apiKey)
	return err
}

// CheckCachedURLs checks which of urls (each a search result's own
// downloadUrl) are already cached on the configured TorBox usenet server,
// for callers deciding which candidate is worth racing (see
// acquisition.Service.prioritizeCachedNZB). Returns an empty map (never an
// error the caller must special-case) if no usenet server is configured.
//
// Now returns map[string]*debrid.NZBInstantAvailInfo instead of plain bool
// so callers can see file count/size without a second API call. Results are
// cached (instantAvailCache) to avoid redundant API calls within the TTL.
func (svc *Service) CheckCachedURLs(ctx context.Context, urls []string) (map[string]*debrid.NZBInstantAvailInfo, error) {
	server, err := svc.pickServer()
	if err != nil {
		return map[string]*debrid.NZBInstantAvailInfo{}, nil
	}

	hashToURL := make(map[string]string, len(urls))
	hashes := make([]string, 0, len(urls))
	for _, u := range urls {
		h := fmt.Sprintf("%x", md5.Sum([]byte(u)))
		hashToURL[h] = u
		hashes = append(hashes, h)
	}

	// Check local cache first — avoids hitting TorBox for hashes we've
	// already looked up recently (whether they were cached or not).
	now := time.Now()
	var missing []string
	out := make(map[string]*debrid.NZBInstantAvailInfo, len(hashes))
	svc.iaMu.Lock()
	for _, h := range hashes {
		if entry, ok := svc.instantAvailCache[h]; ok && entry.expiresAt.After(now) {
			if entry.info != nil {
				if u, ok2 := hashToURL[h]; ok2 {
					out[u] = entry.info
				}
			}
			// nil entry = negative cache (checked, not cached) — skip
		} else {
			missing = append(missing, h)
		}
	}
	svc.iaMu.Unlock()

	if len(missing) > 0 {
		cachedHashes, err := svc.torboxClient.CheckCachedUsenet(ctx, server.APIKey, missing)
		if err != nil {
			return nil, err
		}

		// Cache results (both positive and negative) and populate output.
		svc.iaMu.Lock()
		expiresAt := now.Add(instantAvailTTL)
		for _, h := range missing {
			if info, ok := cachedHashes[h]; ok {
				svc.instantAvailCache[h] = &instantAvailEntry{info: info, expiresAt: expiresAt}
				if u, ok2 := hashToURL[h]; ok2 {
					out[u] = info
				}
			} else {
				// Negative cache — remember the "not cached" result.
				if svc.instantAvailCache[h] == nil || svc.instantAvailCache[h].info != nil {
					svc.instantAvailCache[h] = &instantAvailEntry{info: nil, expiresAt: expiresAt}
				}
			}
		}
		svc.iaMu.Unlock()
	}
	return out, nil
}

// isNZBFailureCached checks whether this URL hash has a recent failure
// cached (so callers can skip re-submitting known-bad content).
func (svc *Service) isNZBFailureCached(u string) bool {
	h := fmt.Sprintf("%x", md5.Sum([]byte(u)))
	svc.fcMu.Lock()
	defer svc.fcMu.Unlock()
	t, ok := svc.failureCache[h]
	return ok && time.Now().Before(t)
}

// IsURLFailed is the public version of isNZBFailureCached — callers in the
// acquisition package use it to skip candidates whose download URLs recently
// failed.
func (svc *Service) IsURLFailed(u string) bool {
	return svc.isNZBFailureCached(u)
}

// recordNZBFailure records a URL hash as having failed, so future attempts
// skip it for failureCacheTTL. Evicts oldest entries when over the cap.
func (svc *Service) recordNZBFailure(u string) {
	h := fmt.Sprintf("%x", md5.Sum([]byte(u)))
	svc.fcMu.Lock()
	defer svc.fcMu.Unlock()
	svc.failureCache[h] = time.Now().Add(failureCacheTTL)
	// Evict oldest entries when over the cap to prevent unbounded growth.
	if len(svc.failureCache) > failureCacheSize {
		var oldestK string
		var oldestV time.Time
		for k, v := range svc.failureCache {
			if oldestK == "" || v.Before(oldestV) {
				oldestK = k
				oldestV = v
			}
		}
		delete(svc.failureCache, oldestK)
	}
}

// RecordURLFailure is the public version of recordNZBFailure — callers in
// the acquisition package use it to mark download URLs as having failed.
func (svc *Service) RecordURLFailure(u string) {
	svc.recordNZBFailure(u)
}

// StartBackgroundSync starts a continuous background goroutine that
// periodically calls syncFromTorBox — one API call fetches every usenet
// download on the account, and syncFromTorBox reconciles them with Vorn's
// own records. This mirrors rdt-client's ProviderUpdater: no per-download
// goroutine ever blocks polling; one periodic sweep catches every download
// regardless of whether it was created by Vorn or externally (e.g., via the
// TorBox dashboard), and no download is ever orphaned by a restart.
func (svc *Service) StartBackgroundSync() {
	svc.StartBackgroundSyncWithInterval(defaultBackgroundSyncInterval)
}

// StartBackgroundSyncWithInterval starts a continuous background goroutine
// that periodically calls syncFromTorBox at the given interval.
func (svc *Service) StartBackgroundSyncWithInterval(interval time.Duration) {
	if interval <= 0 {
		interval = defaultBackgroundSyncInterval
	}
	svc.syncMu.Lock()
	defer svc.syncMu.Unlock()
	if svc.syncCancel != nil {
		return // already running
	}
	ctx, cancel := context.WithCancel(context.Background())
	svc.syncCancel = cancel
	// Run an immediate sync on startup (the old one-shot ReconcileFromTorBox
	// behavior), then every interval thereafter. The syncRunning
	// mutex prevents overlapping sweeps: if the previous sync is still
	// running when the ticker fires, the tick is silently skipped.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("nzb: background sync panicked: %v — sync will retry on next tick", r)
			}
		}()
		svc.runSync(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("nzb: background sync panicked: %v — sync will retry on next tick", r)
						}
					}()
					svc.runSync(ctx)
				}()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Close stops the background sync goroutine. Safe to call multiple times;
// after Close returns, the sync goroutine has stopped and no further sync
// sweeps will run.
// Subscribe returns a receive-only channel and an unsubscribe function.
// The channel receives an event whenever a download completes via the
// background sync. The caller must call the returned function when done
// to avoid leaking the channel.
func (svc *Service) Subscribe() (<-chan NZBDownloadEvent, func()) {
	ch := make(chan NZBDownloadEvent, 1)
	svc.subMu.Lock()
	svc.subscribers[ch] = struct{}{}
	svc.subMu.Unlock()
	return ch, func() {
		svc.subMu.Lock()
		delete(svc.subscribers, ch)
		svc.subMu.Unlock()
		close(ch)
	}
}

func (svc *Service) broadcast() {
	svc.subMu.Lock()
	defer svc.subMu.Unlock()
	for ch := range svc.subscribers {
		select {
		case ch <- NZBDownloadEvent{}:
		default:
			// Channel buffer is full — subscriber isn't consuming fast
			// enough; skip this event rather than blocking the sync.
		}
	}
}

// Close stops the background sync goroutine. Safe to call multiple times;
// after Close returns, the sync goroutine has stopped and no further sync
// sweeps will run.
// Close stops the background sync goroutine. Safe to call multiple times;
// after Close returns, the sync goroutine has stopped and no further sync
// sweeps will run.

// TriggerSync fires an immediate one-shot syncFromTorBox sweep (in the
// caller's goroutine — not in the background poller) and returns when it
// completes. Used by the admin UI's "Recheck TorBox" button to catch
// completions without waiting for the next background tick.
func (svc *Service) TriggerSync() {
	// Use the running sync's context (not a fresh one) so it's still bounded
	// by the same lifecycle — closing the service also cancels this.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	svc.runSync(ctx)
}

func (svc *Service) Close() {
	svc.syncMu.Lock()
	defer svc.syncMu.Unlock()
	if svc.syncCancel != nil {
		svc.syncCancel()
		svc.syncCancel = nil
	}
}

// runSync is a thin wrapper that acquires syncRunning before calling
// syncFromTorBox, ensuring at most one sweep is in-flight at a time.
// Non-blocking: if the previous sweep is still running, this returns
// immediately (the tick was skipped).
func (svc *Service) runSync(ctx context.Context) {
	if !svc.syncRunning.TryLock() {
		return
	}
	defer svc.syncRunning.Unlock()
	svc.syncFromTorBox(ctx)
}

// isDownloadComplete checks whether a TorBox usenet download has finished
// in a way that makes its files available for streaming. Mirrors AIOStreams'
// status computation: downloadFinished + downloadPresent covers the normal
// case, and a downloadState starting with "direct unpack: completed" catches
// the transient post-download state where TorBox has finished downloading
// but hasn't yet set downloadPresent — files ARE ready in this state.
func isDownloadComplete(dl *debrid.TBUsenetInfo) bool {
	if dl.DownloadFinished && dl.DownloadPresent {
		return true
	}
	return strings.HasPrefix(strings.ToLower(dl.DownloadState), "direct unpack: completed")
}

// syncFromTorBox (formerly ReconcileFromTorBox) is the heart of the
// continuous polling approach. Called on startup and every
// backgroundSyncInterval thereafter, it fetches every usenet download on
// the TorBox account (both active and queued — mirroring rdt-client's
// GetCurrentUsenet + GetQueuedUsenet) and reconciles them with Vorn's DB:
//   - "repairing" downloads that TorBox says are done → record files, match
//     WebDAV, finish + promote.
//   - Downloads on TorBox not in Vorn's DB → auto-import with library
//     auto-assignment.
//   - Downloads in Vorn's DB not on TorBox (neither active nor queued) →
//     mark as removed (expired/deleted externally).
//   - Failed downloads → ensure Vorn's error status matches.
//
// This is the Go equivalent of rdt-client's UpdateRdData: one full sweep
// catches everything.
func (svc *Service) syncFromTorBox(ctx context.Context) {
	server, err := svc.pickServer()
	if err != nil {
		return
	}

	// Fetch both active and queued downloads — TorBox may queue a download
	// before it starts actively processing it, and a download that's only
	// queued (not yet active) won't appear in the plain /usenet/mylist
	// response. rdt-client's GetDownloads does this as four separate calls
	// (current + queued for both torrents and usenet); we only need the two
	// usenet variants.
	active, err := svc.torboxClient.ListUsenetDownloads(ctx, server.APIKey)
	if err != nil {
		log.Printf("nzb: sync: listing active usenet downloads: %v", err)
		return
	}
	queued, err := svc.torboxClient.ListQueuedUsenetDownloads(ctx, server.APIKey)
	if err != nil {
		log.Printf("nzb: sync: listing queued usenet downloads: %v", err)
		// Don't abort — active downloads are still useful even if the
		// queued endpoint failed.
	}

	// Combine active + queued into one unified list for reconciliation.
	remote := active
	remote = append(remote, queued...)

	// Build a provider_ref → nzb_download lookup from Vorn's own records.
	// provider_ref is stored as "usenetID:hash" — extract just the
	// usenet ID for matching against TorBox's id field.
	local, _ := svc.store.ListNZBDownloads()
	byRef := make(map[string]*store.NZBDownload, len(local))
	for _, d := range local {
		if d.ProviderRef == "" {
			continue
		}
		refID := d.ProviderRef
		if idx := strings.Index(refID, ":"); idx >= 0 {
			refID = refID[:idx]
		}
		byRef[refID] = d
	}

	// Track which remote IDs we've seen so we can detect expired downloads
	// (local records not present on TorBox anymore).
	seenRemote := make(map[string]bool, len(remote))

	for _, dl := range remote {
		ref := strconv.Itoa(dl.ID)
		seenRemote[ref] = true
		existing := byRef[ref]

		switch {
		case isDownloadComplete(&dl) && !dl.Failed():
			if existing != nil {
				if existing.Status == "repairing" || (existing.Status == "completed" && !existing.Promoted) {
					svc.catchUpDownload(ctx, existing, &dl, server)
				}
			} else {
				svc.importOrphanedDownload(ctx, &dl, server, ref)
			}
		case dl.Failed():
			if existing != nil && existing.Status != "error" {
				svc.finish(existing, fmt.Errorf("torbox: download failed remotely: %s", dl.DownloadState))
			}
		default:
			// Download is still in progress — update progress for any
			// local record that's tracking it.
			if existing != nil && existing.Status == "repairing" {
				if err := svc.store.UpdateNZBProgress(existing.ID, 10000, int64(dl.Progress*10000), "repairing"); err != nil {
					log.Printf("nzb: sync: updating progress for %s: %v", existing.ID, err)
				}
			}
		}
	}

	// Auto-delete: any local record with a provider_ref that wasn't in the
	// remote list (neither active nor queued) has been deleted/expired from
	// TorBox. Also clean up stale "repairing" downloads — if TorBox no
	// longer knows about a download Vorn still thinks is in progress, it
	// was deleted externally (e.g., from the TorBox dashboard).
	for ref, d := range byRef {
		if !seenRemote[ref] {
			switch {
			case d.Status == "repairing":
				log.Printf("nzb: sync: %s (%s) disappeared from torbox while repairing — marking error", d.ID, d.Name)
				svc.finish(d, fmt.Errorf("torbox: download disappeared — may have been deleted externally"))
			case d.Status == "completed" && d.Promoted:
				log.Printf("nzb: sync: %s (%s) no longer on torbox — marking removed", d.ID, d.Name)
				if err := svc.store.RemoveNZBDownload(d.ID); err != nil {
					log.Printf("nzb: sync: removing expired %s: %v", d.ID, err)
				}
			}
		}
	}
}

// catchUpDownload handles a download that exists both locally and on
// TorBox, and TorBox says it's done: record files, request CDN stream URLs,
// match WebDAV, finish, and promote. Extracted from syncFromTorBox to keep
// the main loop readable.
func (svc *Service) catchUpDownload(ctx context.Context, existing *store.NZBDownload, dl *debrid.TBUsenetInfo, server *store.UsenetServer) {
	if existing.LibraryID == nil {
		if parsed := scanner.ParseFilename(existing.Name); parsed.Kind == "movie" || parsed.Kind == "episode" {
			libs, _ := svc.store.ListLibraries()
			for _, lib := range libs {
				if parsed.Kind == "movie" && lib.Type == "movie" {
					existing.LibraryID = &lib.ID
					break
				}
				if parsed.Kind == "episode" && lib.Type == "series" {
					existing.LibraryID = &lib.ID
					break
				}
			}
			if existing.LibraryID != nil {
				if err := svc.store.SetNZBDownloadLibrary(existing.ID, *existing.LibraryID); err != nil {
					log.Printf("nzb: sync: setting library for %s: %v", existing.ID, err)
				}
			}
		}
	}

	log.Printf("nzb: sync: %s (%s) catching up (status=%s promoted=%v download_state=%s)", existing.ID, existing.Name, existing.Status, existing.Promoted, dl.DownloadState)

	// Skip files already recorded — on a repeat sweep, AddNZBFile would
	// otherwise create duplicates.
	existingFiles, _ := svc.store.ListNZBFiles(existing.ID)
	existingByName := make(map[string]bool, len(existingFiles))
	for _, ef := range existingFiles {
		existingByName[ef.Name] = true
	}

	for _, f := range dl.Files {
		name := f.Name
		if name == "" {
			name = f.ShortName
		}
		if existingByName[name] {
			continue
		}
		link, err := svc.torboxClient.RequestUsenetDownloadLink(ctx, server.APIKey, dl.ID, f.ID)
		if err != nil {
			log.Printf("nzb: sync: requesting download link for %s: %v", existing.ID, err)
			link = ""
		}
		if _, err := svc.store.AddNZBFile(existing.ID, name, f.Size, link); err != nil {
			log.Printf("nzb: sync: recording file for %s: %v", existing.ID, err)
		}
		// Mark as seen so subsequent files in the same sweep don't
		// duplicate either (the same file can appear twice in the API
		// response under different IDs, rare but observed).
		existingByName[name] = true
	}
	files, _ := svc.store.ListNZBFiles(existing.ID)
	if len(files) > 0 {
		svc.matchWebDAVURLs(ctx, existing)
		if svc.onComplete != nil {
			if used := svc.onComplete(existing); !used {
				log.Printf("nzb: sync: %s (%s) completed but content verification failed, marking error", existing.ID, existing.Name)
				svc.deleteFromTorBox(existing.ID, existing.ProviderRef)
				svc.finish(existing, fmt.Errorf("content verification failed — wrong file matched to this item"))
				return
			}
		}
		svc.finish(existing, nil)
		svc.broadcast()
	}
}

// tryImmediateCompletion polls TorBox once for a just-created usenet
// download — if it's already complete (the common case for cached NZBs),
// record files, request download links, match WebDAV, and promote
// immediately instead of waiting for the next sync sweep (up to 30s).
// Best-effort: any failure is silently ignored; the regular sync will
// catch it on the next tick.
func (svc *Service) tryImmediateCompletion(ctx context.Context, rec *store.NZBDownload, server *store.UsenetServer, usenetID int) {
	// Give TorBox a moment to process — cached content still needs a
	// sub-second server-side resolution before files are available.
	select {
	case <-time.After(500 * time.Millisecond):
	case <-ctx.Done():
		return
	}

	dl, err := svc.torboxClient.UsenetInfo(ctx, server.APIKey, usenetID)
	if err != nil {
		return
	}
	if dl == nil || !isDownloadComplete(dl) || dl.Failed() {
		return
	}

	log.Printf("nzb: immediate completion for %s (%s) — download already cached", rec.ID, rec.Name)

	// Mirror catchUpDownload: record files, request CDN links, match
	// WebDAV, finish, promote.
	svc.catchUpDownload(ctx, rec, dl, server)
}



// importOrphanedDownload handles a download that exists on TorBox but not
// in Vorn's DB: creates a local record, auto-assigns a library by filename
// parsing, requests CDN stream URLs, matches WebDAV, and promotes.
// Extracted from syncFromTorBox to keep the main loop readable.
func (svc *Service) importOrphanedDownload(ctx context.Context, dl *debrid.TBUsenetInfo, server *store.UsenetServer, ref string) {
	name := dl.Files[0].Name
	if name == "" && len(dl.Files) > 0 {
		name = dl.Files[0].ShortName
	}
	if name == "" {
		name = fmt.Sprintf("usenet-%d", dl.ID)
	}

	var libraryID *string
	if parsed := scanner.ParseFilename(name); parsed.Kind == "movie" || parsed.Kind == "episode" {
		libs, _ := svc.store.ListLibraries()
		for _, lib := range libs {
			if parsed.Kind == "movie" && lib.Type == "movie" {
				lid := lib.ID
				libraryID = &lid
				break
			}
			if parsed.Kind == "episode" && lib.Type == "series" {
				lid := lib.ID
				libraryID = &lid
				break
			}
		}
	}

	rec, err := svc.store.CreateNZBDownload(store.CreateNZBDownloadInput{
		Name:      name,
		LibraryID: libraryID,
	})
	if err != nil {
		log.Printf("nzb: sync: creating record for %d: %v", dl.ID, err)
		return
	}
	if err := svc.store.SetNZBDownloadProviderRef(rec.ID, ref); err != nil {
		log.Printf("nzb: sync: setting provider ref for %s: %v", rec.ID, err)
	}

	for _, f := range dl.Files {
		fn := f.Name
		if fn == "" {
			fn = f.ShortName
		}
		link, err := svc.torboxClient.RequestUsenetDownloadLink(ctx, server.APIKey, dl.ID, f.ID)
		if err != nil {
			log.Printf("nzb: sync: requesting download link for %s: %v", rec.ID, err)
			link = ""
		}
		if _, err := svc.store.AddNZBFile(rec.ID, fn, f.Size, link); err != nil {
			log.Printf("nzb: sync: recording file for %s: %v", rec.ID, err)
		}
	}

	svc.matchWebDAVURLs(ctx, rec)

	if libraryID != nil && svc.onComplete != nil {
		if used := svc.onComplete(rec); !used {
			log.Printf("nzb: sync: %s (%s) orphaned download content verification failed, cleaning up", rec.ID, rec.Name)
			svc.deleteFromTorBox(rec.ID, rec.ProviderRef)
			if err := svc.store.RemoveNZBDownload(rec.ID); err != nil {
				log.Printf("nzb: sync: removing failed orphan %s: %v", rec.ID, err)
			}
			return
		}
	}
	svc.finish(rec, nil)
	svc.broadcast()
	log.Printf("nzb: sync: created record %s for orphaned torbox download %d (library=%v)", rec.ID, dl.ID, libraryID)
}

func (svc *Service) pickServer() (*store.UsenetServer, error) {
	servers, err := svc.store.ListUsenetServers()
	if err != nil {
		return nil, err
	}
	for _, s := range servers {
		if s.Enabled {
			return s, nil
		}
	}
	return nil, fmt.Errorf("nzb: no enabled usenet server configured")
}

func (svc *Service) finish(rec *store.NZBDownload, err error) {
	if ferr := svc.store.FinishNZBDownload(rec.ID, err); ferr != nil {
		log.Printf("nzb: finishing %s: %v", rec.ID, ferr)
	}
}

// List returns every non-removed NZB download.
func (svc *Service) List() ([]*store.NZBDownload, error) {
	return svc.store.ListNZBDownloads()
}

// Remove deletes id from TorBox's own account (best-effort -- logged and
// ignored on failure, since a stale/already-gone remote item shouldn't
// block removing Vorn's own record of it) before marking it removed
// locally. TorBox never puts files on local disk, so there is nothing else
// to clean up.
func (svc *Service) Remove(id string) error {
	rec, err := svc.store.GetNZBDownload(id)
	if err != nil {
		return err
	}
	svc.deleteFromTorBox(id, rec.ProviderRef)
	return svc.store.RemoveNZBDownload(id)
}

// deleteFromTorBox is Remove's cleanup step, factored out so run() can reuse
// it the moment a resolve turns out not to have been used (lost a race,
// failed content verification, no video file) instead of leaking that
// quota/storage until someone happens to remove the item manually later.
func (svc *Service) deleteFromTorBox(id, providerRef string) {
	if providerRef == "" {
		return
	}
	// provider_ref is stored as "<usenetID>:<webdavHash>" (see runTorBox).
	// For backwards compatibility with older records that just have the ID,
	// parse the usenet ID from before the first colon.
	refPart := providerRef
	if idx := strings.Index(providerRef, ":"); idx >= 0 {
		refPart = providerRef[:idx]
	}
	usenetID, cerr := strconv.Atoi(refPart)
	if cerr != nil {
		log.Printf("nzb: parsing provider ref for %s: %v", id, cerr)
		return
	}
	server, serr := svc.pickServer()
	if serr != nil {
		log.Printf("nzb: no enabled server to delete %s from TorBox: %v", id, serr)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), providerDeleteTimeout)
	defer cancel()
	if derr := svc.torboxClient.DeleteUsenetDownload(ctx, server.APIKey, usenetID); derr != nil {
		log.Printf("nzb: deleting %s (%s) from torbox: %v", id, providerRef, derr)
	}
}

func (svc *Service) AddServer(in store.UsenetServer) (*store.UsenetServer, error) {
	return svc.store.CreateUsenetServer(in)
}

func (svc *Service) ListServers() ([]*store.UsenetServer, error) {
	return svc.store.ListUsenetServers()
}

func (svc *Service) RemoveServer(id string) error {
	return svc.store.DeleteUsenetServer(id)
}

func (svc *Service) UpdateServer(id string, in store.UpdateUsenetServerInput) (*store.UsenetServer, error) {
	return svc.store.UpdateUsenetServer(id, in)
}

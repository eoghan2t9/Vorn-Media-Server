package nzb

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const providerDeleteTimeout = 30 * time.Second

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
}

// NewService takes torboxLimiter (see debrid.Service.TorBoxLimiter) rather
// than constructing its own, so NZB's TorBox usenet-caching client shares
// the exact same rate budget as debrid.Service's TorBox debrid-resolve
// client and torrent.Service's TorBox indexer search.
func NewService(st *store.Store, torboxLimiter *debrid.Limiter) *Service {
	svc := &Service{store: st, torboxClient: debrid.NewTorBoxClient(torboxLimiter)}
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
func (svc *Service) CheckCachedURLs(ctx context.Context, urls []string) (map[string]bool, error) {
	server, err := svc.pickServer()
	if err != nil {
		return map[string]bool{}, nil
	}

	hashToURL := make(map[string]string, len(urls))
	hashes := make([]string, 0, len(urls))
	for _, u := range urls {
		h := fmt.Sprintf("%x", md5.Sum([]byte(u)))
		hashToURL[h] = u
		hashes = append(hashes, h)
	}

	cachedHashes, err := svc.torboxClient.CheckCachedUsenet(ctx, server.APIKey, hashes)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(cachedHashes))
	for h := range cachedHashes {
		if u, ok := hashToURL[h]; ok {
			out[u] = true
		}
	}
	return out, nil
}

// ReconcileFromTorBox queries TorBox for every existing usenet download and
// ensures Vorn's nzb_downloads/nzb_files rows reflect reality. Called once
// at startup (and after every reconfigure that rebuilds the NZB service),
// it catches downloads that completed while the process was down, or whose
// in-memory polling goroutines were lost across a restart — without this,
// those downloads stay on TorBox forever consuming quota/storage while Vorn
// has no record of them at all.
func (svc *Service) ReconcileFromTorBox(ctx context.Context) {
	server, err := svc.pickServer()
	if err != nil {
		return
	}

	remote, err := svc.torboxClient.ListUsenetDownloads(ctx, server.APIKey)
	if err != nil {
		log.Printf("nzb: reconciling from torbox: listing usenet downloads: %v", err)
		return
	}
	if len(remote) == 0 {
		return
	}

	log.Printf("nzb: reconciling %d usenet download(s) from torbox", len(remote))

	// Build a provider_ref → nzb_download lookup from Vorn's own records.
	// provider_ref is now stored as "usenetID:hash" — extract just the
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

	for _, dl := range remote {
		ref := strconv.Itoa(dl.ID)
		existing := byRef[ref]

		// If Vorn already has this download and it's finished (completed or
		// errored on TorBox's side), ensure Vorn's own status agrees and
		// that it was marked promoted. A download that Vorn still thinks is
		// "repairing" after a restart definitely finished while the process
		// was down — promote it now.
		switch {
		case dl.DownloadFinished && dl.DownloadPresent && !dl.Failed():
			if existing != nil {
				if existing.Status == "repairing" || (existing.Status == "completed" && !existing.Promoted) {
					// This download finished while Vorn was offline (status
					// still "repairing"), or it completed but was never
					// promoted (e.g. the old IsProbableHash filter blocked
					// it). Either way, ensure files are recorded, CDN stream
					// URLs are fetched (same as runTorBox), WebDAV URLs are
					// matched, and promotion runs.
					log.Printf("nzb: reconciliation: %s (%s) catching up (status=%s promoted=%v)", existing.ID, existing.Name, existing.Status, existing.Promoted)
					for _, f := range dl.Files {
						name := f.Name
						if name == "" {
							name = f.ShortName
						}
						link, err := svc.torboxClient.RequestUsenetDownloadLink(ctx, server.APIKey, dl.ID, f.ID)
						if err != nil {
							log.Printf("nzb: reconciliation: requesting download link for %s: %v", existing.ID, err)
							// Still record the file even without a CDN URL —
							// WebDAV matching below may still find it.
							link = ""
						}
						if _, err := svc.store.AddNZBFile(existing.ID, name, f.Size, link); err != nil {
							log.Printf("nzb: reconciliation: recording file for %s: %v", existing.ID, err)
						}
					}
				files, _ := svc.store.ListNZBFiles(existing.ID)
					if len(files) > 0 {
						svc.matchWebDAVURLs(ctx, existing)
						svc.finish(existing, nil)
						if svc.onComplete != nil {
							svc.onComplete(existing)
						}
					}
				}
			} else if existing == nil {
				// TorBox has a completed download Vorn knows nothing about —
				// create a record and promote it so the files aren't stranded.
				name := dl.Files[0].Name
				if name == "" && len(dl.Files) > 0 {
					name = dl.Files[0].ShortName
				}
				if name == "" {
					name = fmt.Sprintf("usenet-%d", dl.ID)
				}
				rec, err := svc.store.CreateNZBDownload(store.CreateNZBDownloadInput{
					Name: name,
				})
				if err != nil {
					log.Printf("nzb: reconciliation: creating record for %d: %v", dl.ID, err)
					continue
				}
				if err := svc.store.SetNZBDownloadProviderRef(rec.ID, ref); err != nil {
					log.Printf("nzb: reconciliation: setting provider ref for %s: %v", rec.ID, err)
				}
				for _, f := range dl.Files {
					name := f.Name
					if name == "" {
						name = f.ShortName
					}
					if _, err := svc.store.AddNZBFile(rec.ID, name, f.Size, ""); err != nil {
						log.Printf("nzb: reconciliation: recording file for %s: %v", rec.ID, err)
					}
				}
				// These orphaned downloads have no library association —
				// promoteStreamFiles needs one for PromoteCompleted, so this
				// is best-effort: the files appear in the admin NZB list and
				// the admin can assign them to a library or delete them.
				// The stream URLs aren't fetched (no CDN links requested),
				// but the file list is there and visible.
				svc.finish(rec, nil)
				log.Printf("nzb: reconciliation: created record %s for orphaned torbox download %d", rec.ID, dl.ID)
			}
		case dl.Failed():
			if existing != nil && existing.Status != "error" {
				svc.finish(existing, fmt.Errorf("torbox: download failed remotely: %s", dl.DownloadState))
			}
		}
	}
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

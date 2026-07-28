package nzb

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strconv"
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
	usenetID, cerr := strconv.Atoi(providerRef)
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

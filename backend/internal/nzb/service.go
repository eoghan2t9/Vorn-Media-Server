package nzb

import (
	"bytes"
	"context"
	"fmt"
	"log"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// Service resolves NZB releases via a configured TorBox account: parsing
// the index, handing it to TorBox for server-side caching/repair, then
// recording the direct stream URLs TorBox returns and promoting the result
// into the library on completion. No article data ever passes through
// Vorn itself.
type Service struct {
	store      *store.Store
	onComplete func(*store.NZBDownload)
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
	svc.onComplete = func(n *store.NZBDownload) {
		if n.MediaItemID == nil {
			PromoteCompleted(st, n)
			return
		}
		mediaItem, err := AuthorizedMediaItem(st, n)
		if err != nil {
			log.Printf("nzb: checking whether %s is still authoritative for %s: %v", n.ID, *n.MediaItemID, err)
			return
		}
		if mediaItem == nil {
			log.Printf("nzb: %s is a stale/abandoned download, a later attempt already took over -- skipping promotion", n.ID)
			return
		}
		if mediaItem.Kind == "season" {
			PromoteSeasonPackToExistingItems(st, mediaItem, n)
			return
		}
		PromoteToExistingItem(st, mediaItem, n)
	}
	return svc
}

// AddNZB parses a .nzb file's bytes and starts downloading it in the
// background against whichever configured Usenet server is enabled --
// the manual, admin-driven flow (Admin > NZB), which always
// filename-guess-promotes into libraryID at large (see PromoteCompleted).
func (svc *Service) AddNZB(data []byte, libraryID *string) (*store.NZBDownload, error) {
	return svc.addNZB(data, libraryID, nil)
}

// AddNZBForItem is AddNZB's on-demand-acquisition counterpart (the NZB
// analog of debrid.Service.AddLink): it targets a specific existing
// placeholder media_item rather than filename-guessing into a library,
// via mediaItemID/AuthorizedMediaItem fencing (see promote.go).
func (svc *Service) AddNZBForItem(data []byte, libraryID, mediaItemID string) (*store.NZBDownload, error) {
	return svc.addNZB(data, &libraryID, &mediaItemID)
}

func (svc *Service) addNZB(data []byte, libraryID, mediaItemID *string) (*store.NZBDownload, error) {
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

	go svc.run(rec, data)
	return rec, nil
}

func (svc *Service) run(rec *store.NZBDownload, data []byte) {
	server, err := svc.pickServer()
	if err != nil {
		svc.finish(rec, err)
		return
	}
	svc.runTorBox(rec, data, server)
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

// Remove marks a download removed. TorBox never puts files on local disk,
// so there is nothing to clean up beyond the record itself.
func (svc *Service) Remove(id string) error {
	return svc.store.RemoveNZBDownload(id)
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

package prowlarr

import (
	"context"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/nzb"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

const (
	// syncInterval is short (unlike e.g. metadata.Scheduler's 15 minutes)
	// because tick's ListIndexers call hits Prowlarr's own co-located
	// instance directly (a local Docker-network request, not a rate-limited
	// third-party API) -- there's no meaningful cost to polling it often,
	// and doing so is what makes "add a tracker in Prowlarr, it just shows
	// up in Vorn" actually feel automatic rather than requiring a wait (or
	// a restart) for the next sync.
	syncInterval  = 30 * time.Second
	apiKeyPoll    = 5 * time.Second
	apiKeyTimeout = 5 * time.Minute
)

// SyncService periodically mirrors every enabled indexer configured inside
// Prowlarr into Vorn's own indexer tables -- torrent-protocol ones into
// Torrent Indexers, usenet-protocol ones into NZB Indexers -- so adding a
// tracker or indexer in Prowlarr's UI is all that's needed; no manual
// copy-paste into either of Vorn's admin pages. Either service may be nil
// (that protocol just isn't synced) if Vorn's own torrent/NZB acquisition
// isn't enabled.
type SyncService struct {
	torrent    *torrent.Service
	nzb        *nzb.Service
	baseURL    string
	apiKey     string
	configPath string
}

// NewSyncService: exactly one of apiKey or configPath is expected to be
// set. apiKey is used directly if present -- works identically on Docker,
// Windows, Linux, and macOS, no file access needed. Otherwise configPath is
// polled and parsed for Prowlarr's auto-generated key, which only makes
// sense when Vorn can actually reach that file (the bundled Docker Compose
// profile mounts it read-only; a bare-metal deployment could point
// configPath at wherever its own natively-installed Prowlarr keeps
// config.xml).
func NewSyncService(t *torrent.Service, n *nzb.Service, baseURL, apiKey, configPath string) *SyncService {
	return &SyncService{torrent: t, nzb: n, baseURL: baseURL, apiKey: apiKey, configPath: configPath}
}

// Run blocks until ctx is done -- call it with `go`.
func (s *SyncService) Run(ctx context.Context) {
	apiKey := s.apiKey
	if apiKey == "" {
		key, err := WaitForAPIKey(ctx, s.configPath, apiKeyPoll, apiKeyTimeout)
		if err != nil {
			log.Printf("prowlarr sync: %v -- giving up (set VORN_PROWLARR_API_KEY to skip reading config.xml)", err)
			return
		}
		apiKey = key
	}

	s.tick(ctx, apiKey)
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx, apiKey)
		}
	}
}

// tick fetches Prowlarr's current indexer list and registers any enabled
// one Vorn doesn't already have into the matching service for its protocol
// -- by name, "Prowlarr: " prefixed, which doubles as both the dedupe key
// and a visible hint in Vorn's admin pages of where the entry came from. It
// never removes or disables a Vorn indexer, even if it disappears from
// Prowlarr, so a user who's since customized or manually re-added it isn't
// fought.
func (s *SyncService) tick(ctx context.Context, apiKey string) {
	indexers, err := ListIndexers(ctx, s.baseURL, apiKey)
	if err != nil {
		log.Printf("prowlarr sync: listing indexers: %v", err)
		return
	}

	if s.torrent != nil {
		s.syncTorrent(ctx, indexers, apiKey)
	}
	if s.nzb != nil {
		s.syncNZB(ctx, indexers, apiKey)
	}
}

func (s *SyncService) syncTorrent(ctx context.Context, indexers []Indexer, apiKey string) {
	existing, err := s.torrent.ListIndexers()
	if err != nil {
		log.Printf("prowlarr sync: listing existing torrent indexers: %v", err)
		return
	}
	existingNames := make(map[string]bool, len(existing))
	for _, e := range existing {
		existingNames[e.Name] = true
	}

	for _, idx := range indexers {
		if !idx.Enable || idx.Protocol != "torrent" {
			continue
		}
		name := "Prowlarr: " + idx.Name
		if existingNames[name] {
			continue
		}
		if _, err := s.torrent.AddIndexer(ctx, name, IndexerProxyURL(s.baseURL, idx.ID), apiKey, "torznab"); err != nil {
			log.Printf("prowlarr sync: adding torrent indexer %q: %v", name, err)
			continue
		}
		log.Printf("prowlarr sync: registered torrent indexer %q", name)
	}
}

func (s *SyncService) syncNZB(ctx context.Context, indexers []Indexer, apiKey string) {
	existing, err := s.nzb.ListIndexers()
	if err != nil {
		log.Printf("prowlarr sync: listing existing NZB indexers: %v", err)
		return
	}
	existingNames := make(map[string]bool, len(existing))
	for _, e := range existing {
		existingNames[e.Name] = true
	}

	for _, idx := range indexers {
		if !idx.Enable || idx.Protocol != "usenet" {
			continue
		}
		name := "Prowlarr: " + idx.Name
		if existingNames[name] {
			continue
		}
		if _, err := s.nzb.AddIndexer(ctx, name, IndexerProxyURL(s.baseURL, idx.ID), apiKey); err != nil {
			log.Printf("prowlarr sync: adding NZB indexer %q: %v", name, err)
			continue
		}
		log.Printf("prowlarr sync: registered NZB indexer %q", name)
	}
}

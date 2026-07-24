package prowlarr

import (
	"context"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

const (
	syncInterval  = 30 * time.Minute
	apiKeyPoll    = 5 * time.Second
	apiKeyTimeout = 5 * time.Minute
)

// SyncService periodically mirrors every enabled torrent-protocol indexer
// configured inside Prowlarr into Vorn's own torrent indexer table, so
// adding a tracker in Prowlarr's UI is all that's needed -- no manual
// copy-paste into Vorn's Torrent Indexers admin page. Usenet-protocol
// indexers configured in Prowlarr are left alone; NZB indexers are a
// separate integration this doesn't touch.
type SyncService struct {
	torrent    *torrent.Service
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
func NewSyncService(t *torrent.Service, baseURL, apiKey, configPath string) *SyncService {
	return &SyncService{torrent: t, baseURL: baseURL, apiKey: apiKey, configPath: configPath}
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
// torrent-protocol one Vorn doesn't already have -- by name, "Prowlarr: "
// prefixed, which doubles as both the dedupe key and a visible hint in the
// Torrent Indexers admin page of where the entry came from. It never
// removes or disables a Vorn indexer, even if it disappears from Prowlarr,
// so a user who's since customized or manually re-added it isn't fought.
func (s *SyncService) tick(ctx context.Context, apiKey string) {
	indexers, err := ListIndexers(ctx, s.baseURL, apiKey)
	if err != nil {
		log.Printf("prowlarr sync: listing indexers: %v", err)
		return
	}

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
		if _, err := s.torrent.AddIndexer(name, TorznabBaseURL(s.baseURL, idx.ID), apiKey); err != nil {
			log.Printf("prowlarr sync: adding indexer %q: %v", name, err)
			continue
		}
		log.Printf("prowlarr sync: registered indexer %q", name)
	}
}

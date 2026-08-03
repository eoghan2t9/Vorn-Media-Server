package httpapi

import (
	"context"
	"log"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/nzb"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

const indexerDisabledReason = "indexer does not support IMDb or TVDB id search"

// sweepIndexerCapabilities re-checks every torrent/NZB indexer's IMDb/TVDB
// id-search capability once at startup (called once from NewServer, not on
// every reconfigure -- an admin saving an unrelated integration setting
// shouldn't re-test every indexer) and disables, with a reason, any enabled
// one that supports neither. Vorn's acquisition system only ever searches
// indexers by id (resolveImdbSearchParams in acquisition/service.go), so an
// indexer lacking both is silently useless even while "enabled" -- this is
// what catches an indexer added before this capability check existed, or
// one whose real caps changed since it was added. Runs unconditionally
// every startup (cheap -- one HTTP round-trip per indexer) rather than as a
// separate scheduled job, both self-healing and consistent with
// torrent.Service's own resumeActive() re-check on every boot.
func (s *Server) sweepIndexerCapabilities() {
	if torrents, err := s.store.ListTorrentIndexers(); err != nil {
		log.Printf("httpapi: listing torrent indexers for capability sweep: %v", err)
	} else {
		for _, idx := range torrents {
			// TorBox-provider indexers speak TorBox's own IMDb-ID-driven
			// search API directly, not Torznab -- no caps document to fetch.
			if !idx.Enabled || idx.Provider == "torbox" {
				continue
			}
			caps, err := torrent.TestIndexer(context.Background(), idx.BaseURL, idx.APIKey)
			if err != nil {
				log.Printf("httpapi: capability sweep: testing torrent indexer %q: %v", idx.Name, err)
				continue
			}
			if caps.SupportsImdb || caps.SupportsTvdb {
				continue
			}
			s.disableIndexerForCapability(idx.ID, idx.Name, "torrent", func(reason string) error {
				enabled := false
				_, err := s.store.UpdateTorrentIndexer(idx.ID, store.UpdateTorrentIndexerInput{Enabled: &enabled, DisabledReason: &reason})
				return err
			})
		}
	}

	if indexers, err := s.store.ListNZBIndexers(); err != nil {
		log.Printf("httpapi: listing nzb indexers for capability sweep: %v", err)
	} else {
		for _, idx := range indexers {
			if !idx.Enabled {
				continue
			}
			caps, err := nzb.TestIndexer(context.Background(), idx.BaseURL, idx.APIKey)
			if err != nil {
				log.Printf("httpapi: capability sweep: testing nzb indexer %q: %v", idx.Name, err)
				continue
			}
			if caps.SupportsImdb || caps.SupportsTvdb {
				continue
			}
			s.disableIndexerForCapability(idx.ID, idx.Name, "nzb", func(reason string) error {
				enabled := false
				_, err := s.store.UpdateNZBIndexer(idx.ID, store.UpdateNZBIndexerInput{Enabled: &enabled, DisabledReason: &reason})
				return err
			})
		}
	}
}

func (s *Server) disableIndexerForCapability(id, name, kind string, update func(reason string) error) {
	if err := update(indexerDisabledReason); err != nil {
		log.Printf("httpapi: capability sweep: disabling %s indexer %q: %v", kind, name, err)
		return
	}
	log.Printf("httpapi: capability sweep: disabled %s indexer %q (%s)", kind, name, indexerDisabledReason)
	s.notify.Send(context.Background(), "indexer_disabled", map[string]any{
		"id": id, "name": name, "kind": kind, "reason": indexerDisabledReason,
	})
}

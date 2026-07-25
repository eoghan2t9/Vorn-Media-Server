package acquisition

import (
	"context"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// monitorInterval mirrors backup.Scheduler's own reasoning for its
// checkInterval: fine-grained enough that toggling monitoring on
// something takes effect within the hour, not needing a restart, while not
// so tight it hammers indexers/TMDb for items that rarely change.
const monitorInterval = 30 * time.Minute

// MonitorScheduler periodically re-checks every monitored movie/series:
// grabbing newly-aired episodes or retrying a still-unavailable movie, and
// re-searching already-owned monitored items for a better release than
// what they currently have (auto-swapping in, per the same fenced
// resolve-and-promote path Acquire itself uses). Mirrors backup.Scheduler's
// shape exactly (a single boot-started ticker goroutine).
type MonitorScheduler struct {
	store   *store.Store
	service *Service
}

// NewMonitorScheduler is a method on Service (not a free function) so
// callers already holding an acquisition.Service don't need to separately
// thread its dependencies through again.
func (s *Service) NewMonitorScheduler(st *store.Store) *MonitorScheduler {
	return &MonitorScheduler{store: st, service: s}
}

// Run blocks, ticking on startup and then every monitorInterval, until ctx
// is cancelled. Meant to be started in its own goroutine.
func (m *MonitorScheduler) Run(ctx context.Context) {
	m.tick(ctx)
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

func (m *MonitorScheduler) tick(ctx context.Context) {
	m.grabNewContent(ctx)
	m.checkQualityUpgrades(ctx)
}

// grabNewContent re-syncs every monitored series (picking up newly-aired
// episodes from TMDb) and re-acquires anything still placeholder/error
// under it, plus retries any monitored movie that still has no file.
func (m *MonitorScheduler) grabNewContent(ctx context.Context) {
	items, err := m.store.ListMonitoredTopLevel()
	if err != nil {
		log.Printf("acquisition: monitor: listing monitored items: %v", err)
		return
	}

	for _, item := range items {
		switch item.Kind {
		case "movie":
			if item.AcquisitionStatus == "placeholder" || item.AcquisitionStatus == "error" {
				if err := m.service.Acquire(ctx, item.ID); err != nil {
					log.Printf("acquisition: monitor: re-acquiring movie %s: %v", item.ID, err)
				}
			}
		case "series":
			if err := m.service.SyncSeriesTree(ctx, item); err != nil {
				log.Printf("acquisition: monitor: syncing series tree for %s: %v", item.ID, err)
				continue
			}
			m.grabPlaceholderEpisodes(ctx, item)
		}
	}
}

// grabPlaceholderEpisodes walks seriesItem's season/episode tree and
// acquires anything not yet owned. A season with more than one pending
// episode tries a single season-pack release first (AcquireSeasonPack,
// which itself falls back to per-episode acquisition if no pack resolves);
// a season with exactly one pending episode just acquires it directly --
// no point searching for a pack to grab one episode.
func (m *MonitorScheduler) grabPlaceholderEpisodes(ctx context.Context, seriesItem *store.MediaItem) {
	seasons, err := m.store.ListChildren(seriesItem.ID)
	if err != nil {
		log.Printf("acquisition: monitor: listing seasons for %s: %v", seriesItem.ID, err)
		return
	}
	for _, season := range seasons {
		episodes, err := m.store.ListChildren(season.ID)
		if err != nil {
			log.Printf("acquisition: monitor: listing episodes for season %s: %v", season.ID, err)
			continue
		}
		var pending []*store.MediaItem
		for _, ep := range episodes {
			if ep.AcquisitionStatus == "placeholder" || ep.AcquisitionStatus == "error" {
				pending = append(pending, ep)
			}
		}
		switch {
		case len(pending) == 0:
			continue
		case len(pending) == 1:
			if err := m.service.Acquire(ctx, pending[0].ID); err != nil {
				log.Printf("acquisition: monitor: acquiring episode %s: %v", pending[0].ID, err)
			}
		default:
			if err := m.service.AcquireSeasonPack(ctx, season.ID); err != nil {
				log.Printf("acquisition: monitor: season pack for %s: %v", season.ID, err)
			}
		}
	}
}

// checkQualityUpgrades re-searches every monitored, already-owned
// movie/episode for a strictly-better release than what it currently has,
// and swaps it in automatically on success. A failure or "nothing better
// found" is silent -- the existing file is untouched either way, and the
// next tick tries again.
func (m *MonitorScheduler) checkQualityUpgrades(ctx context.Context) {
	items, err := m.store.ListMonitoredOwned()
	if err != nil {
		log.Printf("acquisition: monitor: listing monitored owned items: %v", err)
		return
	}
	for _, item := range items {
		if err := m.service.checkUpgrade(ctx, item); err != nil {
			log.Printf("acquisition: monitor: checking upgrade for %s: %v", item.ID, err)
		}
	}
}

// checkUpgrade compares the current best available release for item
// against what it's already playing (parsed from CurrentReleaseTitle) and,
// if strictly better by resolution, resolves it through the same fenced
// attempt Acquire's candidates use -- reusing tryCandidate/tryNZBCandidate
// means a failed upgrade attempt can never touch item's existing path
// (promotion only ever writes success when this attempt is still the
// authoritative one when it finishes). Torrent is tried first (checked
// only if s.torrent is configured); NZB is checked independently after,
// even if torrent already found something, in case the NZB tier's best
// candidate happens to itself be an upgrade over the (possibly still
// torrent-current) release -- either successful upgrade returns
// immediately rather than trying both.
func (s *Service) checkUpgrade(ctx context.Context, item *store.MediaItem) error {
	s.ensureExpectedRuntime(ctx, item)

	query, err := s.buildSearchQuery(item)
	if err != nil {
		return err
	}
	profile, err := s.store.GetQualityProfile(item.LibraryID)
	if err != nil {
		return err
	}
	currentTier, _ := resolutionTier(ParseResolution(item.CurrentReleaseTitle))

	if s.torrent != nil {
		searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
		candidates, err := s.torrent.Search(searchCtx, query)
		cancel()
		if err != nil {
			return err
		}
		ranked := ScoreAndRank(candidates, profile)
		if len(ranked) > 0 {
			best := ranked[0]
			if newTier, known := resolutionTier(best.Resolution); known && newTier > currentTier {
				account, err := s.pickDebridAccount()
				if err != nil {
					return err
				}
				if s.tryCandidate(item, account, best) {
					s.notifySend("upgraded", map[string]any{"itemId": item.ID, "title": item.Title, "release": best.Title})
					return nil
				}
			}
		}
	}

	if s.nzb != nil {
		searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
		candidates, err := s.nzb.Search(searchCtx, query)
		cancel()
		if err != nil {
			return err
		}
		ranked := ScoreAndRankNZB(candidates, profile)
		if len(ranked) > 0 {
			best := ranked[0]
			if newTier, known := resolutionTier(best.Resolution); known && newTier > currentTier {
				fetchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
				ok := s.tryNZBCandidate(fetchCtx, item, best)
				cancel()
				if ok {
					s.notifySend("upgraded", map[string]any{"itemId": item.ID, "title": item.Title, "release": best.Title})
				}
			}
		}
	}
	return nil
}

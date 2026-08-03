package acquisition

import (
	"context"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
)

// linkProbeTimeout bounds a single liveness check (both attempts --
// transcode.ProbeWithRetry's own two 8s sub-timeouts plus its 2s gap fit
// comfortably inside 20s) in refreshDeadLinks. Still short relative to a
// full sweep since this runs against every owned remote item on every
// tick; a provider that's slow to respond on both attempts shouldn't
// stall the whole sweep waiting on one title (that title's link is just
// deemed dead this round, same as any other timeout/error, and gets a
// fresh resolve attempted).
const linkProbeTimeout = 20 * time.Second

// MonitorScheduler periodically re-checks every monitored movie/series:
// grabbing newly-aired episodes or retrying a still-unavailable movie, and
// re-searching already-owned monitored items for a better release than
// what they currently have (auto-swapping in, per the same fenced
// resolve-and-promote path Acquire itself uses).
type MonitorScheduler struct {
	store    *store.Store
	service  *Service
	interval time.Duration
}

// NewMonitorScheduler is a method on Service (not a free function) so
// callers already holding an acquisition.Service don't need to separately
// thread its dependencies through again.
func (s *Service) NewMonitorScheduler(st *store.Store) *MonitorScheduler {
	return s.NewMonitorSchedulerWithInterval(st, 1800) // 30 min default
}

// NewMonitorSchedulerWithInterval creates a MonitorScheduler with a
// custom tick interval (in seconds). Values <= 0 fall back to the default.
func (s *Service) NewMonitorSchedulerWithInterval(st *store.Store, intervalSecs int) *MonitorScheduler {
	interval := 30 * time.Minute
	if intervalSecs > 0 {
		interval = time.Duration(intervalSecs) * time.Second
	}
	return &MonitorScheduler{store: st, service: s, interval: interval}
}

// Run blocks, ticking on startup and then every m.interval, until ctx
// is cancelled. Meant to be started in its own goroutine.
func (m *MonitorScheduler) Run(ctx context.Context) {
	m.tick(ctx)
	ticker := time.NewTicker(m.interval)
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
	m.refreshDeadLinks(ctx)
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

// refreshDeadLinks proactively re-resolves any owned movie/episode whose
// cached stream link (media_items.path) no longer probes successfully --
// the same ffprobe-based liveness check handlePlayItem uses reactively
// on a play attempt (see stream.go), just run ahead of time here so a
// viewer is much less likely to ever wait through a fresh resolve
// themselves. Unlike grabNewContent/checkQualityUpgrades above, this
// covers every owned remote item regardless of monitored status -- a
// dead debrid/NZB link is worth fixing whether or not the title is being
// watched for new episodes/upgrades. Uses ReacquireSoftFail, not
// Reacquire: this is a background hygiene pass with no viewer waiting on
// the result, so a failed re-resolve (a real dead end, or just every
// indexer/provider transiently rate-limited by checking many items at
// once) reverts the item to 'owned' instead of demoting it to 'error' --
// never worse off than before this ran, and still self-heals reactively
// the next time someone actually presses play.
func (m *MonitorScheduler) refreshDeadLinks(ctx context.Context) {
	items, err := m.store.ListOwnedRemoteItems()
	if err != nil {
		log.Printf("acquisition: monitor: listing owned remote items: %v", err)
		return
	}
	for _, item := range items {
		if item.Path == nil {
			continue
		}
		probeStart := time.Now()
		probeCtx, cancel := context.WithTimeout(ctx, linkProbeTimeout)
		_, err := transcode.ProbeWithRetry(probeCtx, *item.Path)
		cancel()
		if err == nil {
			continue
		}
		log.Printf("acquisition: monitor: link dead for %s (%s) after %v: %v -- re-resolving", item.ID, item.Title, time.Since(probeStart), err)
		if err := m.service.ReacquireSoftFail(ctx, item.ID); err != nil {
			log.Printf("acquisition: monitor: refreshing dead link for %s: %v", item.ID, err)
		}
	}
}

// checkUpgrade compares the current best available release for item
// against what it's already playing (parsed from CurrentReleaseTitle) and,
// if strictly better by resolution, resolves it through the same
// atomic-claim promotion path Acquire's candidates use (racing a
// single-element slice) -- a failed upgrade attempt can never touch item's
// existing path, since promotion only ever writes on a successful claim.
// Torrent is tried first (checked
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
				accounts, err := s.listEnabledDebridAccounts()
				if err != nil {
					return err
				}
				if s.raceTorrentCandidates(item, accounts, []ScoredRelease{best}) {
					s.notifySend("upgraded", map[string]any{"itemId": item.ID, "title": item.Title, "release": best.Title})
					_ = s.store.RecordUpgrade(item.ID, item.Title, item.CurrentReleaseTitle, best.Title, "torrent")
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
				if s.raceNZBCandidates(item, []ScoredNZBRelease{best}) {
					s.notifySend("upgraded", map[string]any{"itemId": item.ID, "title": item.Title, "release": best.Title})
					_ = s.store.RecordUpgrade(item.ID, item.Title, item.CurrentReleaseTitle, best.Title, "nzb")
				}
			}
		}
	}
	return nil
}

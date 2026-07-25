package acquisition

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/notify"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/nzb"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

const (
	searchTimeout = 30 * time.Second
	// candidateTimeout bounds how long runAcquire waits on a single
	// candidate's resolve before giving up and trying the next one --
	// independent of debrid.Service's own internal resolveTimeout (20 min),
	// which keeps running in the background regardless (see
	// waitForOutcome's doc comment for why that's safe).
	candidateTimeout = 5 * time.Minute
	// candidateTimeoutNZB is far more generous than candidateTimeout: a real
	// NZB download+repair genuinely takes minutes (bounded by connection
	// speed and file size), unlike debrid's near-instant cache-check, so 5
	// minutes would give up on most perfectly-good downloads before they
	// could ever finish.
	candidateTimeoutNZB = 20 * time.Minute
	outcomePoll         = 2 * time.Second
	// maxCandidates caps a single Acquire's worst-case background runtime
	// to roughly maxCandidates * candidateTimeout.
	maxCandidates = 5
	// maxNZBCandidates is far lower than maxCandidates: trying 5 sequential
	// candidateTimeoutNZB-bounded downloads is impractical (worst case
	// hours) -- the first couple either work or the Usenet provider has a
	// real problem no amount of retrying fixes.
	maxNZBCandidates = 2
)

// Service turns a TMDb title a user has opened or pressed play on into a
// browsable placeholder and, on play, a real playable stream: search
// torrent indexers, score candidates against the owning library's quality
// profile, and hand the winner to a debrid provider to resolve. It sits
// above torrent/debrid/metadata rather than living inside any one of them,
// since it's genuinely orchestration across all three.
type Service struct {
	store   *store.Store
	tmdb    *metadata.TMDbClient
	torrent *torrent.Service
	nzb     *nzb.Service
	debrid  *debrid.Service
	notify  *notify.Service
}

func NewService(st *store.Store, tmdb *metadata.TMDbClient, t *torrent.Service, n *nzb.Service, d *debrid.Service, notifySvc *notify.Service) *Service {
	return &Service{store: st, tmdb: tmdb, torrent: t, nzb: n, debrid: d, notify: notifySvc}
}

// MaterializePlaceholder finds or creates the local placeholder media_item
// for a TMDb movie/series a user has opened, browsing or played -- keyed on
// (library, kind, tmdb_id) so opening the same browse card twice is a
// no-op. A freshly-created series placeholder also gets its season/episode
// tree synced immediately so its detail page has something to show.
func (s *Service) MaterializePlaceholder(ctx context.Context, libraryID string, tmdbID int, kind string) (*store.MediaItem, error) {
	if kind != "movie" && kind != "series" {
		return nil, fmt.Errorf("acquisition: unsupported kind %q", kind)
	}

	if existing, err := s.store.GetMediaItemByTmdbID(libraryID, tmdbID, kind); err == nil {
		if kind == "series" {
			if err := s.SyncSeriesTree(ctx, existing); err != nil {
				log.Printf("acquisition: re-syncing series tree for %s: %v", existing.ID, err)
			}
			return s.store.GetMediaItem(existing.ID)
		}
		return existing, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	if kind == "movie" {
		return s.materializeMovie(ctx, libraryID, tmdbID)
	}
	return s.materializeSeries(ctx, libraryID, tmdbID)
}

func (s *Service) materializeMovie(ctx context.Context, libraryID string, tmdbID int) (*store.MediaItem, error) {
	details, err := s.tmdb.GetMovieDetails(ctx, tmdbID)
	if err != nil {
		return nil, fmt.Errorf("acquisition: fetching movie details: %w", err)
	}
	item, err := s.store.CreatePlaceholder(store.CreatePlaceholderInput{
		LibraryID: libraryID, Kind: "movie", Title: details.Title, TmdbID: &tmdbID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.applyTopLevelMetadata(item.ID, details.Overview, details.ReleaseDate, details.PosterURL, details.BackdropURL, details.Runtime, details.ImdbID); err != nil {
		log.Printf("acquisition: applying metadata to %s: %v", item.ID, err)
	}
	return s.store.GetMediaItem(item.ID)
}

func (s *Service) materializeSeries(ctx context.Context, libraryID string, tmdbID int) (*store.MediaItem, error) {
	details, err := s.tmdb.GetSeriesDetails(ctx, tmdbID)
	if err != nil {
		return nil, fmt.Errorf("acquisition: fetching series details: %w", err)
	}
	item, err := s.store.CreatePlaceholder(store.CreatePlaceholderInput{
		LibraryID: libraryID, Kind: "series", Title: details.Title, TmdbID: &tmdbID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.applyTopLevelMetadata(item.ID, details.Overview, details.FirstAirDate, details.PosterURL, details.BackdropURL, 0, details.ImdbID); err != nil {
		log.Printf("acquisition: applying metadata to %s: %v", item.ID, err)
	}
	fresh, err := s.store.GetMediaItem(item.ID)
	if err != nil {
		return nil, err
	}
	if err := s.SyncSeriesTree(ctx, fresh); err != nil {
		log.Printf("acquisition: syncing series tree for %s: %v", fresh.ID, err)
	}
	return fresh, nil
}

func (s *Service) applyTopLevelMetadata(itemID, overview, releaseDate, posterURL, backdropURL string, runtimeMinutes int, imdbID string) error {
	update := store.MetadataUpdate{Overview: overview, PosterURL: posterURL, BackdropURL: backdropURL, RuntimeMinutes: runtimeMinutes, ImdbID: imdbID}
	if d, err := time.Parse("2006-01-02", releaseDate); err == nil {
		update.ReleaseDate = &d
	}
	return s.store.ApplyMetadata(itemID, update, false)
}

// ensureExpectedRuntime backfills item's (movie) or its episodes' (season/
// episode) TMDb-reported runtime if missing, so debrid/nzb's promote.go
// content-verification check (comparing a resolved release's actual probed
// duration against media_items.metadata->>'runtimeMinutes') has something
// to compare against -- an item materialized before this field existed
// would otherwise silently skip verification forever. Best-effort: a
// failed lookup just leaves it nil, letting verification skip for this one
// attempt rather than blocking acquisition on a TMDb hiccup.
func (s *Service) ensureExpectedRuntime(ctx context.Context, item *store.MediaItem) {
	switch item.Kind {
	case "movie":
		if (item.RuntimeMinutes != nil && item.ImdbID != nil) || item.TmdbID == nil {
			return
		}
		details, err := s.tmdb.GetMovieDetails(ctx, *item.TmdbID)
		if err != nil {
			log.Printf("acquisition: backfilling runtime/imdb id for %s: %v", item.ID, err)
			return
		}
		update := store.MetadataUpdate{RuntimeMinutes: details.Runtime, ImdbID: details.ImdbID}
		if update.RuntimeMinutes > 0 || update.ImdbID != "" {
			if err := s.store.ApplyMetadata(item.ID, update, false); err != nil {
				log.Printf("acquisition: applying backfilled runtime/imdb id to %s: %v", item.ID, err)
			}
		}
	case "episode":
		if item.ParentID == nil {
			return
		}
		season, err := s.store.GetMediaItem(*item.ParentID)
		if err != nil || season.ParentID == nil {
			return
		}
		if item.RuntimeMinutes != nil {
			// Episode runtime already known -- still worth checking whether
			// the series itself is missing ImdbID/TvdbID (e.g. it was
			// backfilled for runtime before these fields existed), but skip
			// the TMDb round-trip entirely if the series already has both.
			series, err := s.store.GetMediaItem(*season.ParentID)
			if err == nil && series.ImdbID != nil && series.TvdbID != nil {
				return
			}
		}
		s.backfillSeriesMetadata(ctx, *season.ParentID)
	case "season":
		if item.ParentID == nil {
			return
		}
		s.backfillSeriesMetadata(ctx, *item.ParentID)
	}
}

// backfillSeriesMetadata re-syncs seriesID's whole episode tree, which (once
// GetSeasonDetails/SyncSeriesTree parse/apply the Runtime field) backfills
// every episode's expected runtime in one shot, not just the one being
// acquired, and backfills the series' own ImdbID the same way (see
// SyncSeriesTree) -- SyncSeriesTree is already idempotent/"safe to call
// repeatedly".
func (s *Service) backfillSeriesMetadata(ctx context.Context, seriesID string) {
	series, err := s.store.GetMediaItem(seriesID)
	if err != nil {
		log.Printf("acquisition: loading series %s to backfill runtimes: %v", seriesID, err)
		return
	}
	if err := s.SyncSeriesTree(ctx, series); err != nil {
		log.Printf("acquisition: backfilling episode runtimes for series %s: %v", seriesID, err)
	}
}

// SyncSeriesTree fetches seriesItem's season/episode list from TMDb and
// find-or-creates placeholder rows for each, safe to call repeatedly (a
// season/episode that already exists -- placeholder or, from a real scan,
// already-owned -- is left untouched). Season 0 ("specials") is already
// dropped by GetSeriesDetails.
//
// Episode identity intentionally mirrors scan-promoted episodes exactly
// (title = "S%02dE%02d", the same key PromoteEpisode uses): if this used
// TMDb's per-episode name as the title instead, re-running this after a
// metadata update had already renamed the row would fail to find it by its
// old title and insert a duplicate. The real TMDb episode name is stored in
// overview instead, where it can safely change on every resync.
func (s *Service) SyncSeriesTree(ctx context.Context, seriesItem *store.MediaItem) error {
	if seriesItem.TmdbID == nil {
		return fmt.Errorf("acquisition: series %s has no tmdb id", seriesItem.ID)
	}
	details, err := s.tmdb.GetSeriesDetails(ctx, *seriesItem.TmdbID)
	if err != nil {
		return fmt.Errorf("acquisition: fetching series details: %w", err)
	}

	// A whole series shares one IMDb ID and one TheTVDB ID -- TV search
	// (torrent.SearchByIMDb, nzb.SearchByIMDb) only ever needs the series'
	// own ID(s) plus season/episode numbers, never a separate per-episode
	// one -- so this backfills both on the series row itself using the
	// GetSeriesDetails call already made above, not a second TMDb request.
	if (seriesItem.ImdbID == nil && details.ImdbID != "") || (seriesItem.TvdbID == nil && details.TvdbID > 0) {
		update := store.MetadataUpdate{ImdbID: details.ImdbID, TvdbID: details.TvdbID}
		if err := s.store.ApplyMetadata(seriesItem.ID, update, false); err != nil {
			log.Printf("acquisition: applying backfilled imdb/tvdb id to %s: %v", seriesItem.ID, err)
		}
	}

	for _, season := range details.Seasons {
		seasonNum := season.SeasonNumber
		seasonTitle := fmt.Sprintf("Season %d", seasonNum)

		seasonItem, err := s.store.FindPlaceholderChild(seriesItem.LibraryID, &seriesItem.ID, "season", seasonTitle, &seasonNum, nil)
		if errors.Is(err, store.ErrNotFound) {
			seasonItem, err = s.store.CreatePlaceholder(store.CreatePlaceholderInput{
				LibraryID: seriesItem.LibraryID, ParentID: &seriesItem.ID, Kind: "season",
				Title: seasonTitle, SeasonNumber: &seasonNum, Monitored: seriesItem.Monitored,
			})
		}
		if err != nil {
			return fmt.Errorf("acquisition: season %d: %w", seasonNum, err)
		}

		episodes, err := s.tmdb.GetSeasonDetails(ctx, *seriesItem.TmdbID, seasonNum)
		if err != nil {
			log.Printf("acquisition: fetching season %d details for %s: %v", seasonNum, seriesItem.ID, err)
			continue
		}
		for _, ep := range episodes {
			epNum := ep.EpisodeNumber
			epTitle := fmt.Sprintf("S%02dE%02d", seasonNum, epNum)

			existing, err := s.store.FindPlaceholderChild(seriesItem.LibraryID, &seasonItem.ID, "episode", epTitle, &seasonNum, &epNum)
			if errors.Is(err, store.ErrNotFound) {
				existing, err = s.store.CreatePlaceholder(store.CreatePlaceholderInput{
					LibraryID: seriesItem.LibraryID, ParentID: &seasonItem.ID, Kind: "episode",
					Title: epTitle, SeasonNumber: &seasonNum, EpisodeNumber: &epNum, Monitored: seriesItem.Monitored,
				})
			}
			if err != nil {
				log.Printf("acquisition: episode S%02dE%02d for %s: %v", seasonNum, epNum, seriesItem.ID, err)
				continue
			}
			if ep.Overview != "" || ep.Runtime > 0 {
				update := store.MetadataUpdate{Overview: ep.Overview, RuntimeMinutes: ep.Runtime}
				if err := s.store.ApplyMetadata(existing.ID, update, false); err != nil {
					log.Printf("acquisition: applying episode metadata to %s: %v", existing.ID, err)
				}
			}
		}
	}
	return nil
}

// Acquire kicks off search->score->resolve for a single movie or episode
// media_item with no file yet. It returns as soon as the item has been
// flipped to 'searching' and the background resolve started -- callers
// poll GetMediaItem for status, same pattern debrid.Service.AddLink already
// uses for its own background resolve.
func (s *Service) Acquire(ctx context.Context, itemID string) error {
	return s.startAcquire(itemID, true)
}

// Reacquire is Acquire for an item that's already 'owned' but whose current
// stream link has gone dead -- see httpapi.handlePlayItem, which calls this
// when ffprobe can no longer open a debrid-backed item's URL. It runs the
// exact same search->score->resolve pipeline as a fresh Acquire; the
// existing active_debrid_item_id fencing (see tryCandidate/waitForOutcome)
// is what makes it safe to overwrite an already-'owned' item's path.
func (s *Service) Reacquire(ctx context.Context, itemID string) error {
	return s.startAcquire(itemID, false)
}

// startAcquire is Acquire and Reacquire's shared body -- blockOwned is the
// only thing distinguishing "don't touch it, it's already fulfilled" from
// "it's fulfilled but no longer good, replace it".
func (s *Service) startAcquire(itemID string, blockOwned bool) error {
	item, err := s.store.GetMediaItem(itemID)
	if err != nil {
		return err
	}
	switch item.AcquisitionStatus {
	case "searching", "acquiring":
		return nil // already in flight
	case "owned":
		if blockOwned {
			return nil
		}
	}

	if err := s.store.SetMediaItemAcquisitionStatus(item.ID, "searching"); err != nil {
		return err
	}
	go s.runAcquire(item)
	return nil
}

// runAcquire tries torrent+debrid first (near-instant when it works), and
// only falls back to NZB/Usenet (a real download, genuinely minutes) if
// torrent fails to produce a working release -- see acquireViaTorrent/
// acquireViaNZB. Either tier is silently skipped (not a failure on its
// own) if that source isn't configured at all.
func (s *Service) runAcquire(item *store.MediaItem) {
	runtimeCtx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	s.ensureExpectedRuntime(runtimeCtx, item)
	cancel()

	query, err := s.buildSearchQuery(item)
	if err != nil {
		s.fail(item.ID, err)
		return
	}
	profile, err := s.store.GetQualityProfile(item.LibraryID)
	if err != nil {
		s.fail(item.ID, err)
		return
	}

	torrentErr := s.acquireViaTorrent(item, query, profile)
	if torrentErr == nil {
		return
	}
	nzbErr := s.acquireViaNZB(item, query, profile)
	if nzbErr == nil {
		return
	}

	s.fail(item.ID, combineAcquireErrors(torrentErr, nzbErr))
	s.notifySend("acquisition_failed", map[string]any{"itemId": item.ID, "title": item.Title})
}

// resolveImdbSearchParams returns the IMDb/TheTVDB IDs and season/episode
// numbers to pass to torrent.SearchByIMDb/nzb.SearchByIMDb for item -- a
// movie's own ImdbID (movies have no TVDB id), or an episode's parent
// series' ImdbID/TvdbID plus the episode's season/episode numbers (a whole
// series shares one of each, see SyncSeriesTree). Both IDs are returned
// when known since which one an indexer actually needs varies: TorBox's
// torrent-search API and many Newznab indexers' movie-search accept IMDb
// ID, but most real-world Newznab indexers' tv-search function keys off
// TheTVDB ID instead (confirmed against a live NZBGeek account -- its own
// caps document doesn't list imdbid as a supported tv-search param at all).
// An empty imdbID/tvdbID means it isn't known yet (not backfilled, or TMDb
// has none on file) -- callers skip whichever query that would drive.
func (s *Service) resolveImdbSearchParams(item *store.MediaItem) (imdbID, tvdbID string, season, episode int) {
	switch item.Kind {
	case "movie":
		if item.ImdbID != nil {
			imdbID = *item.ImdbID
		}
	case "episode":
		if item.ParentID == nil {
			return
		}
		seasonItem, err := s.store.GetMediaItem(*item.ParentID)
		if err != nil || seasonItem.ParentID == nil {
			return
		}
		series, err := s.store.GetMediaItem(*seasonItem.ParentID)
		if err != nil {
			return
		}
		if series.ImdbID != nil {
			imdbID = *series.ImdbID
		}
		if series.TvdbID != nil {
			tvdbID = strconv.Itoa(*series.TvdbID)
		}
		if item.SeasonNumber != nil {
			season = *item.SeasonNumber
		}
		if item.EpisodeNumber != nil {
			episode = *item.EpisodeNumber
		}
	}
	return
}

// acquireViaTorrent searches, then tries up to maxCandidates scored
// releases in order (best first) until one actually resolves into a
// playable file -- not just the single top pick, since a release can look
// good on paper (seeders, resolution) and still fail to resolve (dead
// torrent, provider can't cache it, no video file inside). Returns nil on
// success (having already sent the "acquired" notification).
func (s *Service) acquireViaTorrent(item *store.MediaItem, query string, profile store.QualityProfile) error {
	if s.torrent == nil {
		return ErrTorrentNotConfigured
	}
	searchCtx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	defer cancel()

	candidates, err := s.torrent.Search(searchCtx, query)
	if err != nil {
		return fmt.Errorf("searching indexers: %w", err)
	}
	if imdbID, _, season, episode := s.resolveImdbSearchParams(item); imdbID != "" {
		if imdbCandidates, err := s.torrent.SearchByIMDb(searchCtx, imdbID, season, episode); err != nil {
			log.Printf("acquisition: TorBox indexer search for %s: %v", item.ID, err)
		} else {
			candidates = append(candidates, imdbCandidates...)
		}
	}
	if len(candidates) == 0 {
		return ErrNoSearchResults
	}

	ranked := ScoreAndRank(candidates, profile)
	if len(ranked) == 0 {
		return ErrNoAcceptableRelease
	}
	if len(ranked) > maxCandidates {
		ranked = ranked[:maxCandidates]
	}

	account, err := s.pickDebridAccount()
	if err != nil {
		return err
	}

	for _, candidate := range ranked {
		if s.tryCandidate(item, account, candidate) {
			s.notifySend("acquired", map[string]any{"itemId": item.ID, "title": item.Title, "release": candidate.Title})
			return nil
		}
	}

	// Every candidate either failed to resolve or timed out. A timed-out
	// candidate's debrid.Service.run goroutine may still be running in the
	// background and could succeed later -- clearing active_debrid_item_id
	// fences that off too, same as between candidates, so a late finish
	// can't silently resurrect an item already handed off to the NZB tier.
	if err := s.store.SetMediaItemActiveDebridItem(item.ID, ""); err != nil {
		log.Printf("acquisition: clearing active resolve for %s: %v", item.ID, err)
	}
	return errors.New("no torrent candidate could be resolved")
}

// acquireViaNZB is acquireViaTorrent's NZB counterpart, tried only after it
// fails -- see runAcquire.
func (s *Service) acquireViaNZB(item *store.MediaItem, query string, profile store.QualityProfile) error {
	if s.nzb == nil {
		return ErrNZBNotConfigured
	}
	searchCtx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	defer cancel()

	candidates, err := s.nzb.Search(searchCtx, query)
	if err != nil {
		return fmt.Errorf("searching NZB indexers: %w", err)
	}
	if imdbID, tvdbID, season, episode := s.resolveImdbSearchParams(item); imdbID != "" || tvdbID != "" {
		if imdbCandidates, err := s.nzb.SearchByIMDb(searchCtx, imdbID, tvdbID, season, episode); err != nil {
			log.Printf("acquisition: id-based NZB indexer search for %s: %v", item.ID, err)
		} else {
			candidates = append(candidates, imdbCandidates...)
		}
	}
	if len(candidates) == 0 {
		return ErrNoNZBSearchResults
	}

	ranked := ScoreAndRankNZB(candidates, profile)
	if len(ranked) == 0 {
		return ErrNoAcceptableNZBRelease
	}
	if len(ranked) > maxNZBCandidates {
		ranked = ranked[:maxNZBCandidates]
	}

	for _, candidate := range ranked {
		fetchCtx, cancel := context.WithTimeout(context.Background(), searchTimeout)
		ok := s.tryNZBCandidate(fetchCtx, item, candidate)
		cancel()
		if ok {
			s.notifySend("acquired", map[string]any{"itemId": item.ID, "title": item.Title, "release": candidate.Title})
			return nil
		}
	}

	if err := s.store.SetMediaItemActiveNZBDownload(item.ID, ""); err != nil {
		log.Printf("acquisition: clearing active NZB download for %s: %v", item.ID, err)
	}
	return errors.New("no NZB candidate could be resolved")
}

// combineAcquireErrors reports just the one tier's error when the other
// was never attempted (not configured at all), or both when both were
// tried and failed.
func combineAcquireErrors(torrentErr, nzbErr error) error {
	switch {
	case errors.Is(torrentErr, ErrTorrentNotConfigured):
		return nzbErr
	case errors.Is(nzbErr, ErrNZBNotConfigured):
		return torrentErr
	default:
		return fmt.Errorf("torrent: %v; nzb: %v", torrentErr, nzbErr)
	}
}

// notifySend is a nil-safe wrapper around notify.Service.Send -- s.notify
// is nil whenever acquisition itself is unconfigured (see main.go), and a
// notification is never load-bearing enough to justify a longer-lived
// context than "fire and forget" here.
func (s *Service) notifySend(event string, payload map[string]any) {
	if s.notify == nil {
		return
	}
	s.notify.Send(context.Background(), event, payload)
}

// FulfillRequest fans a content request out into whichever libraries are
// configured as the default standard/4K targets for mediaType, materializing
// a placeholder in each and starting acquisition -- movies acquire directly,
// series are marked monitored so MonitorScheduler's next tick grabs their
// episodes (a bare Acquire call on a series-kind item fails immediately,
// since buildSearchQuery only knows how to search for a movie or episode).
// Meant to be called via `go`: MaterializePlaceholder makes a blocking TMDb
// call, and the HTTP handler that creates the request shouldn't wait on it.
func (s *Service) FulfillRequest(ctx context.Context, requestID, mediaType string, tmdbID int) {
	targets, err := s.store.ListDefaultRequestTargets(mediaType)
	if err != nil {
		log.Printf("acquisition: loading default request targets for %s: %v", mediaType, err)
		return
	}
	for _, lib := range targets {
		item, err := s.MaterializePlaceholder(ctx, lib.ID, tmdbID, mediaType)
		if err != nil {
			log.Printf("acquisition: fulfilling request %s into library %s: %v", requestID, lib.ID, err)
			continue
		}
		if err := s.store.CreateContentRequestFulfillment(requestID, lib.ID, item.ID); err != nil {
			log.Printf("acquisition: recording fulfillment for request %s in library %s: %v", requestID, lib.ID, err)
		}

		switch mediaType {
		case "movie":
			if err := s.Acquire(ctx, item.ID); err != nil {
				log.Printf("acquisition: starting acquire for request %s in library %s: %v", requestID, lib.ID, err)
			}
		case "series":
			if err := s.store.SetMediaItemMonitored(item.ID, true); err != nil {
				log.Printf("acquisition: monitoring series for request %s in library %s: %v", requestID, lib.ID, err)
			}
		}
	}
}

// AcquireSeasonPack tries to fulfil every still-placeholder episode of a
// season with a single season-pack release rather than searching per
// episode. Used only by MonitorScheduler when grabbing new episodes for a
// monitored series -- the interactive single-episode Acquire path (a user
// pressing play) never calls this, so a play click can't get stuck behind
// a multi-GB season download.
//
// If no season-pack candidate is found, or every one fails to resolve or
// produce a release debrid.PromoteSeasonPackToExistingItems can attribute
// any episodes from, this falls back to calling ordinary Acquire on each
// still-placeholder episode individually -- clearing the season's
// active_debrid_item_id first so a season-pack attempt that finishes very
// late can't race the fallback's independent per-episode resolves.
func (s *Service) AcquireSeasonPack(ctx context.Context, seasonID string) error {
	season, err := s.store.GetMediaItem(seasonID)
	if err != nil {
		return err
	}
	if season.Kind != "season" || season.ParentID == nil || season.SeasonNumber == nil {
		return fmt.Errorf("acquisition: %s is not a season", seasonID)
	}
	series, err := s.store.GetMediaItem(*season.ParentID)
	if err != nil {
		return fmt.Errorf("acquisition: loading series for season %s: %w", seasonID, err)
	}
	s.ensureExpectedRuntime(ctx, season)

	if s.tryAcquireSeasonPack(ctx, season, series, *season.SeasonNumber) {
		return nil
	}

	if err := s.store.SetMediaItemActiveDebridItem(season.ID, ""); err != nil {
		log.Printf("acquisition: clearing active resolve for season %s: %v", season.ID, err)
	}
	if err := s.store.SetMediaItemActiveNZBDownload(season.ID, ""); err != nil {
		log.Printf("acquisition: clearing active NZB download for season %s: %v", season.ID, err)
	}

	episodes, err := s.store.ListChildren(season.ID)
	if err != nil {
		return fmt.Errorf("acquisition: listing episodes for season %s: %w", season.ID, err)
	}
	for _, ep := range episodes {
		if ep.AcquisitionStatus != "placeholder" && ep.AcquisitionStatus != "error" {
			continue
		}
		if err := s.Acquire(ctx, ep.ID); err != nil {
			log.Printf("acquisition: falling back to single-episode acquire for %s: %v", ep.ID, err)
		}
	}
	return nil
}

// tryAcquireSeasonPack searches for and tries season-pack candidates for
// one season, trying torrent+debrid first and falling back to NZB/Usenet
// only if that fails -- same tiering as runAcquire (see acquireViaTorrent/
// acquireViaNZB) -- returning true if one of them ends up fulfilling it.
func (s *Service) tryAcquireSeasonPack(ctx context.Context, season, series *store.MediaItem, seasonNumber int) bool {
	if s.trySeasonPackViaTorrent(ctx, season, series, seasonNumber) {
		return true
	}
	return s.trySeasonPackViaNZB(ctx, season, series, seasonNumber)
}

func (s *Service) trySeasonPackViaTorrent(ctx context.Context, season, series *store.MediaItem, seasonNumber int) bool {
	if s.torrent == nil {
		return false
	}
	query := fmt.Sprintf("%s S%02d", series.Title, seasonNumber)
	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	candidates, err := s.torrent.Search(searchCtx, query)
	if err != nil {
		log.Printf("acquisition: searching season pack for %s: %v", season.ID, err)
		return false
	}
	// TorBox's torrent-search API has no "whole season" query mode (season
	// and episode are both required) -- episode=1 is sent as a
	// representative probe, since many real season-pack releases are
	// indexed under every episode they contain. The LooksLikeSingleEpisode
	// filter below applies the same to whatever it finds as to Search's own
	// Torznab results.
	if series.ImdbID != nil {
		if imdbCandidates, err := s.torrent.SearchByIMDb(searchCtx, *series.ImdbID, seasonNumber, 1); err != nil {
			log.Printf("acquisition: TorBox indexer search for season %s: %v", season.ID, err)
		} else {
			candidates = append(candidates, imdbCandidates...)
		}
	}

	var packCandidates []torrent.SearchResult
	for _, c := range candidates {
		if !scanner.LooksLikeSingleEpisode(c.Title) {
			packCandidates = append(packCandidates, c)
		}
	}

	profile, err := s.store.GetQualityProfile(season.LibraryID)
	if err != nil {
		log.Printf("acquisition: loading quality profile for season %s: %v", season.ID, err)
		return false
	}
	ranked := ScoreAndRank(packCandidates, profile)
	if len(ranked) > maxCandidates {
		ranked = ranked[:maxCandidates]
	}
	if len(ranked) == 0 {
		return false
	}

	account, err := s.pickDebridAccount()
	if err != nil {
		log.Printf("acquisition: %v", err)
		return false
	}

	for _, candidate := range ranked {
		if s.tryCandidate(season, account, candidate) {
			s.notifySend("acquired", map[string]any{
				"itemId": season.ID, "title": fmt.Sprintf("%s Season %d", series.Title, seasonNumber), "release": candidate.Title,
			})
			return true
		}
	}
	return false
}

// trySeasonPackViaNZB is trySeasonPackViaTorrent's NZB counterpart, tried
// only after it fails.
func (s *Service) trySeasonPackViaNZB(ctx context.Context, season, series *store.MediaItem, seasonNumber int) bool {
	if s.nzb == nil {
		return false
	}
	query := fmt.Sprintf("%s S%02d", series.Title, seasonNumber)
	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	candidates, err := s.nzb.Search(searchCtx, query)
	if err != nil {
		log.Printf("acquisition: searching NZB season pack for %s: %v", season.ID, err)
		return false
	}
	// Unlike TorBox's torrent-search API, Newznab's t=tvsearch genuinely
	// supports a season-only query (omitting ep) -- most indexers return
	// season packs for exactly this query shape, the same one Sonarr itself
	// sends for a season-pack search.
	if series.ImdbID != nil || series.TvdbID != nil {
		var imdbID, tvdbID string
		if series.ImdbID != nil {
			imdbID = *series.ImdbID
		}
		if series.TvdbID != nil {
			tvdbID = strconv.Itoa(*series.TvdbID)
		}
		if imdbCandidates, err := s.nzb.SearchByIMDb(searchCtx, imdbID, tvdbID, seasonNumber, 0); err != nil {
			log.Printf("acquisition: id-based NZB indexer search for season %s: %v", season.ID, err)
		} else {
			candidates = append(candidates, imdbCandidates...)
		}
	}

	var packCandidates []nzb.SearchResult
	for _, c := range candidates {
		if !scanner.LooksLikeSingleEpisode(c.Title) {
			packCandidates = append(packCandidates, c)
		}
	}

	profile, err := s.store.GetQualityProfile(season.LibraryID)
	if err != nil {
		log.Printf("acquisition: loading quality profile for season %s: %v", season.ID, err)
		return false
	}
	ranked := ScoreAndRankNZB(packCandidates, profile)
	if len(ranked) > maxNZBCandidates {
		ranked = ranked[:maxNZBCandidates]
	}
	if len(ranked) == 0 {
		return false
	}

	for _, candidate := range ranked {
		fetchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
		ok := s.tryNZBCandidate(fetchCtx, season, candidate)
		cancel()
		if ok {
			s.notifySend("acquired", map[string]any{
				"itemId": season.ID, "title": fmt.Sprintf("%s Season %d", series.Title, seasonNumber), "release": candidate.Title,
			})
			return true
		}
	}
	return false
}

// tryCandidate sends one scored release to debrid and waits to see whether
// it specifically is the one that ends up fulfilling item.
func (s *Service) tryCandidate(item *store.MediaItem, account *store.DebridAccount, candidate ScoredRelease) bool {
	// Reset to 'acquiring' at the start of every candidate, not just once
	// before the loop: a previous candidate's failed promotion (e.g. no
	// video file in the release) can have already flipped the item to
	// 'error', which would otherwise make this candidate's own
	// waitForOutcome see that stale status and give up immediately.
	if err := s.store.SetMediaItemAcquisitionStatus(item.ID, "acquiring"); err != nil {
		log.Printf("acquisition: setting status on %s: %v", item.ID, err)
	}

	libraryID, itemID := item.LibraryID, item.ID
	newItem, err := s.debrid.AddLink(debrid.AddLinkInput{
		AccountID:   account.ID,
		SourceRef:   candidate.DownloadURL,
		Name:        candidate.Title,
		LibraryID:   &libraryID,
		MediaItemID: &itemID,
	})
	if err != nil {
		log.Printf("acquisition: sending %q to debrid for %s: %v", candidate.Title, item.ID, err)
		return false
	}
	if err := s.store.SetMediaItemActiveDebridItem(item.ID, newItem.ID); err != nil {
		log.Printf("acquisition: recording active resolve for %s: %v", item.ID, err)
	}
	return s.waitForOutcome(item.ID, candidateTimeout, func() bool {
		di, err := s.store.GetDebridItem(newItem.ID)
		return err == nil && di.Status == "error"
	})
}

// tryNZBCandidate is tryCandidate's NZB counterpart: sends one scored NZB
// release to nzb.Service and waits to see whether it specifically is the
// one that ends up fulfilling item. item may be a movie/episode (single
// file) or a season row (nzb.Service's onComplete branches on
// item.Kind == "season" the same way debrid's does).
func (s *Service) tryNZBCandidate(ctx context.Context, item *store.MediaItem, candidate ScoredNZBRelease) bool {
	if err := s.store.SetMediaItemAcquisitionStatus(item.ID, "acquiring"); err != nil {
		log.Printf("acquisition: setting status on %s: %v", item.ID, err)
	}

	data, err := nzb.FetchNZB(ctx, candidate.DownloadURL)
	if err != nil {
		log.Printf("acquisition: fetching NZB %q for %s: %v", candidate.Title, item.ID, err)
		return false
	}
	rec, err := s.nzb.AddNZBForItem(data, item.LibraryID, item.ID)
	if err != nil {
		log.Printf("acquisition: starting NZB download %q for %s: %v", candidate.Title, item.ID, err)
		return false
	}
	if err := s.store.SetMediaItemActiveNZBDownload(item.ID, rec.ID); err != nil {
		log.Printf("acquisition: recording active NZB download for %s: %v", item.ID, err)
	}
	return s.waitForOutcome(item.ID, candidateTimeoutNZB, func() bool {
		r, err := s.store.GetNZBDownload(rec.ID)
		return err == nil && r.Status == "error"
	})
}

// waitForOutcome polls until either isFailed reports the current resolve
// attempt as terminally failed, or the media_item itself reaches a terminal
// state: 'owned' (this attempt won -- confirmed by the promotion path's own
// active-resolve fencing check, not just "the resolve says ready", since
// promotion can still fail after that, e.g. no video file in the release)
// or 'error' (this attempt's own failed promotion set it). isFailed is the
// one thing that differs between sources: tryCandidate checks
// GetDebridItem(...).Status, tryNZBCandidate checks GetNZBDownload(...).Status.
//
// A candidate that times out here keeps resolving in the background (both
// debrid.Service and nzb.Service have their own longer-running internal
// processes and nothing cancels them from here) -- that's fine: the
// active-resolve fencing (active_debrid_item_id / active_nzb_download_id)
// means a late success or failure from an abandoned attempt is simply
// ignored by promotion once a later candidate (or "give up") has taken over.
func (s *Service) waitForOutcome(mediaItemID string, timeout time.Duration, isFailed func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if isFailed() {
			return false
		}
		if mi, err := s.store.GetMediaItem(mediaItemID); err == nil {
			switch mi.AcquisitionStatus {
			case "owned":
				return true
			case "error":
				return false
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(outcomePoll)
	}
}

func (s *Service) pickDebridAccount() (*store.DebridAccount, error) {
	accounts, err := s.store.ListDebridAccounts()
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if a.Enabled {
			return a, nil
		}
	}
	return nil, errors.New("no enabled debrid account configured")
}

func (s *Service) fail(itemID string, err error) {
	if serr := s.store.SetMediaItemAcquisitionError(itemID, err.Error()); serr != nil {
		log.Printf("acquisition: setting error on %s: %v", itemID, serr)
	}
}

// buildSearchQuery turns a placeholder movie/episode into an indexer search
// string: "{Title} {Year}" for a movie, "{Series Title} S01E02" for an
// episode (walking up to the series row for its title, since an episode
// row only carries its own numbers).
func (s *Service) buildSearchQuery(item *store.MediaItem) (string, error) {
	switch item.Kind {
	case "movie":
		year := ""
		if item.ReleaseDate != nil {
			year = strconv.Itoa(item.ReleaseDate.Year())
		}
		return strings.TrimSpace(item.Title + " " + year), nil
	case "episode":
		if item.ParentID == nil || item.SeasonNumber == nil || item.EpisodeNumber == nil {
			return "", fmt.Errorf("acquisition: episode %s missing season/episode/parent", item.ID)
		}
		season, err := s.store.GetMediaItem(*item.ParentID)
		if err != nil {
			return "", fmt.Errorf("acquisition: loading season for episode %s: %w", item.ID, err)
		}
		if season.ParentID == nil {
			return "", fmt.Errorf("acquisition: season %s has no parent series", season.ID)
		}
		series, err := s.store.GetMediaItem(*season.ParentID)
		if err != nil {
			return "", fmt.Errorf("acquisition: loading series for episode %s: %w", item.ID, err)
		}
		return fmt.Sprintf("%s S%02dE%02d", series.Title, *item.SeasonNumber, *item.EpisodeNumber), nil
	default:
		return "", fmt.Errorf("acquisition: cannot acquire kind %q", item.Kind)
	}
}

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
	outcomePoll      = 2 * time.Second
	// maxCandidates caps a single Acquire's worst-case background runtime
	// to roughly maxCandidates * candidateTimeout.
	maxCandidates = 5
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
	debrid  *debrid.Service
	notify  *notify.Service
}

func NewService(st *store.Store, tmdb *metadata.TMDbClient, t *torrent.Service, d *debrid.Service, n *notify.Service) *Service {
	return &Service{store: st, tmdb: tmdb, torrent: t, debrid: d, notify: n}
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
	if err := s.applyTopLevelMetadata(item.ID, details.Overview, details.ReleaseDate, details.PosterURL, details.BackdropURL); err != nil {
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
	if err := s.applyTopLevelMetadata(item.ID, details.Overview, details.FirstAirDate, details.PosterURL, details.BackdropURL); err != nil {
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

func (s *Service) applyTopLevelMetadata(itemID, overview, releaseDate, posterURL, backdropURL string) error {
	update := store.MetadataUpdate{Overview: overview, PosterURL: posterURL, BackdropURL: backdropURL}
	if d, err := time.Parse("2006-01-02", releaseDate); err == nil {
		update.ReleaseDate = &d
	}
	return s.store.ApplyMetadata(itemID, update, false)
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
			if ep.Overview != "" {
				if err := s.store.ApplyMetadata(existing.ID, store.MetadataUpdate{Overview: ep.Overview}, false); err != nil {
					log.Printf("acquisition: applying episode overview to %s: %v", existing.ID, err)
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

// runAcquire searches, then tries up to maxCandidates scored releases in
// order (best first) until one actually resolves into a playable file --
// not just the single top pick, since a release can look good on paper
// (seeders, resolution) and still fail to resolve (dead torrent, provider
// can't cache it, no video file inside).
func (s *Service) runAcquire(item *store.MediaItem) {
	searchCtx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	defer cancel()

	query, err := s.buildSearchQuery(item)
	if err != nil {
		s.fail(item.ID, err)
		return
	}

	candidates, err := s.torrent.Search(searchCtx, query)
	if err != nil {
		s.fail(item.ID, fmt.Errorf("searching indexers: %w", err))
		return
	}

	profile, err := s.store.GetQualityProfile(item.LibraryID)
	if err != nil {
		s.fail(item.ID, err)
		return
	}

	ranked := ScoreAndRank(candidates, profile)
	if len(ranked) == 0 {
		s.fail(item.ID, ErrNoAcceptableRelease)
		return
	}
	if len(ranked) > maxCandidates {
		ranked = ranked[:maxCandidates]
	}

	account, err := s.pickDebridAccount()
	if err != nil {
		s.fail(item.ID, err)
		return
	}

	for _, candidate := range ranked {
		if s.tryCandidate(item, account, candidate) {
			s.notifySend("acquired", map[string]any{"itemId": item.ID, "title": item.Title, "release": candidate.Title})
			return
		}
	}

	// Every candidate either failed to resolve or timed out. A timed-out
	// candidate's debrid.Service.run goroutine may still be running in the
	// background and could succeed later -- clearing active_debrid_item_id
	// fences that off too, same as between candidates, so a late finish
	// can't silently resurrect an item the user has already been told
	// failed.
	if err := s.store.SetMediaItemActiveDebridItem(item.ID, ""); err != nil {
		log.Printf("acquisition: clearing active resolve for %s: %v", item.ID, err)
	}
	s.fail(item.ID, errors.New("no candidate release could be resolved"))
	s.notifySend("acquisition_failed", map[string]any{"itemId": item.ID, "title": item.Title})
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

	if s.tryAcquireSeasonPack(ctx, season, series, *season.SeasonNumber) {
		return nil
	}

	if err := s.store.SetMediaItemActiveDebridItem(season.ID, ""); err != nil {
		log.Printf("acquisition: clearing active resolve for season %s: %v", season.ID, err)
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
// one season, returning true if one of them ends up fulfilling it.
func (s *Service) tryAcquireSeasonPack(ctx context.Context, season, series *store.MediaItem, seasonNumber int) bool {
	query := fmt.Sprintf("%s S%02d", series.Title, seasonNumber)
	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	candidates, err := s.torrent.Search(searchCtx, query)
	if err != nil {
		log.Printf("acquisition: searching season pack for %s: %v", season.ID, err)
		return false
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
	return s.waitForOutcome(item.ID, newItem.ID, candidateTimeout)
}

// waitForOutcome polls until debridItemID's resolve reaches a terminal
// state Vorn can act on: the media_item became 'owned' (this attempt won
// -- confirmed by debrid.PromoteToExistingItem's own active_debrid_item_id
// fencing check, not just "debrid_item says ready", since promotion can
// still fail after that, e.g. no video file in the resolved release), the
// media_item was set to 'error' by this attempt's own failed promotion,
// the debrid_item itself errored (resolve failed before promotion ever
// ran), or timeout.
//
// A candidate that times out here keeps resolving in the background
// (debrid.Service.run has its own longer internal timeout and nothing
// cancels it from here) -- that's fine: active_debrid_item_id fencing
// means a late success or failure from an abandoned attempt is simply
// ignored by promotion once a later candidate (or "give up") has taken
// over, per PromoteToExistingItem's isAuthoritative check.
func (s *Service) waitForOutcome(mediaItemID, debridItemID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if di, err := s.store.GetDebridItem(debridItemID); err == nil && di.Status == "error" {
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

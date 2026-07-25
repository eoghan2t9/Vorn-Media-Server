package metadata

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// requestSpacing keeps Vorn well under TMDb's documented ~40-50 requests per
// 10 seconds per API key, without needing a full token-bucket limiter for
// what's normally a small, human-triggered batch (one library's worth of
// unmatched titles). MusicBrainz's usage policy (~1 req/sec) and Open
// Library are both comfortably under this same spacing too.
const requestSpacing = 300 * time.Millisecond

// providerBundle groups every credentialed provider that Reconfigure can
// swap out atomically as one unit, so a settings change can never be
// observed half-applied (e.g. a new TMDb client paired with a stale Fanart
// one) -- see Service.providers.
type providerBundle struct {
	provider               Provider
	fallbackSeriesProvider SeriesProvider
	fanart                 *FanartClient
	omdb                   *OMDbClient
}

// Service matches library items against whichever external metadata
// providers are configured. providers is hot-reloadable: Reconfigure
// atomically swaps in a fresh bundle whenever Admin > Integrations credentials
// change, no restart needed (see httpapi.Server.reconfigure, the caller).
// Each provider within a bundle is independently nil-able: a fresh install
// has none of them, a partially-configured one might only have TMDb, etc.
// -- matchItem below skips any kind whose provider is nil rather than
// erroring.
type Service struct {
	store             *store.Store
	musicProvider     MusicProvider
	audiobookProvider AudiobookProvider
	providers         atomic.Pointer[providerBundle]
}

// NewService constructs a Service with no providers configured -- call
// Reconfigure right after to attach whatever credentials are available
// (env-var defaults merged with any DB-saved Admin > Integrations values).
func NewService(st *store.Store) *Service {
	svc := &Service{store: st}
	svc.providers.Store(&providerBundle{}) // safe empty default, never nil
	return svc
}

// WithMusicProvider / WithAudiobookProvider attach the optional music/
// audiobook metadata providers -- separate from the constructor since
// they're gated by their own admin toggle (see IntegrationSettings) rather
// than being required for the service to exist at all like TMDb is. Unlike
// ProviderConfig below, MusicBrainz/Open Library need no credentials, so
// these are attached once at construction and never swapped.
func (svc *Service) WithMusicProvider(p MusicProvider) *Service {
	svc.musicProvider = p
	return svc
}

func (svc *Service) WithAudiobookProvider(p AudiobookProvider) *Service {
	svc.audiobookProvider = p
	return svc
}

// ProviderConfig is the credentialed-provider subset of IntegrationSettings
// (plus any env-var fallback already resolved by the caller) needed to
// (re)build a providerBundle.
type ProviderConfig struct {
	TMDbAPIKey   string
	TVDbAPIKey   string
	TVDbPin      string
	FanartAPIKey string
	OMDbAPIKey   string
}

// currentProviders is the safe read side of providers: a zero-value Service
// (as some tests construct directly, e.g. &Service{}) never calls Store, so
// Load() would return a nil *providerBundle -- fall back to an empty one
// rather than making every call site nil-check.
func (svc *Service) currentProviders() *providerBundle {
	if b := svc.providers.Load(); b != nil {
		return b
	}
	return &providerBundle{}
}

// Reconfigure rebuilds every credentialed provider from cfg and atomically
// swaps them in as one bundle -- called once at Service construction and
// again every time Admin > Integrations credentials change, so a save takes
// effect on the very next sync with no restart.
func (svc *Service) Reconfigure(cfg ProviderConfig) {
	bundle := &providerBundle{}
	if cfg.TMDbAPIKey != "" {
		bundle.provider = NewTMDbProvider(cfg.TMDbAPIKey)
	}
	if cfg.TVDbAPIKey != "" {
		bundle.fallbackSeriesProvider = NewTVDbProvider(cfg.TVDbAPIKey, cfg.TVDbPin)
	}
	if cfg.FanartAPIKey != "" {
		bundle.fanart = NewFanartClient(cfg.FanartAPIKey)
	}
	if cfg.OMDbAPIKey != "" {
		bundle.omdb = NewOMDbClient(cfg.OMDbAPIKey)
	}
	svc.providers.Store(bundle)
}

// StartLibrarySync matches every not-yet-matched, not-locked item in a
// library against whichever metadata provider applies to its kind, in the
// background.
func (svc *Service) StartLibrarySync(libraryID string) (*store.MetadataSyncJob, error) {
	job, err := svc.store.CreateMetadataSyncJob(libraryID)
	if err != nil {
		return nil, err
	}
	go svc.run(job)
	return job, nil
}

func (svc *Service) run(job *store.MetadataSyncJob) {
	ctx := context.Background()

	// Read music/audiobook enablement fresh for this run rather than at
	// service construction time, so toggling either in Admin > Integrations
	// takes effect on the very next sync -- no server restart needed (unlike
	// TMDb/OpenSubtitles, which still require one since their credentialed
	// client is only ever built once at startup).
	musicEnabled, audiobookEnabled := false, false
	if settings, err := svc.store.GetIntegrationSettings(); err != nil {
		log.Printf("metadata: loading integration settings: %v", err)
	} else {
		musicEnabled = settings.MusicMetadataEnabled
		audiobookEnabled = settings.AudiobookMetadataEnabled
	}

	items, err := svc.store.ListItemsNeedingMetadata(job.LibraryID)
	if err != nil {
		svc.finish(job, err)
		return
	}
	if err := svc.store.SetMetadataSyncJobCounts(job.ID, int64(len(items)), 0); err != nil {
		log.Printf("metadata: updating job counts: %v", err)
	}

	var matched int64
	for i, item := range items {
		if i > 0 {
			time.Sleep(requestSpacing)
		}

		matchedNow, err := svc.processItem(ctx, item, musicEnabled, audiobookEnabled)
		if err != nil {
			log.Printf("metadata: matching item %s (%q): %v", item.ID, item.Title, err)
			continue
		}
		if matchedNow {
			matched++
			if err := svc.store.SetMetadataSyncJobCounts(job.ID, int64(len(items)), matched); err != nil {
				log.Printf("metadata: updating job counts: %v", err)
			}
		}
	}

	// Second, cheaper pass: backfill cast/similar onto movies/series that
	// were already matched (have a tmdb_id) before this feature existed, or
	// by an earlier run of this same loop -- skips the title-search step
	// entirely since the id is already known. Runs strictly after the loop
	// above, so an item matched just now is never double-processed here.
	castItems, err := svc.store.ListItemsNeedingCastSync(job.LibraryID)
	if err != nil {
		log.Printf("metadata: listing cast-backfill items: %v", err)
		castItems = nil
	}
	if len(castItems) > 0 {
		if err := svc.store.SetMetadataSyncJobCounts(job.ID, int64(len(items)+len(castItems)), matched); err != nil {
			log.Printf("metadata: updating job counts: %v", err)
		}
	}
	for i, item := range castItems {
		if i > 0 {
			time.Sleep(requestSpacing)
		}
		if err := svc.backfillCastAndSimilar(ctx, item); err != nil {
			log.Printf("metadata: cast-backfill for item %s (%q): %v", item.ID, item.Title, err)
			continue
		}
		matched++
		if err := svc.store.SetMetadataSyncJobCounts(job.ID, int64(len(items)+len(castItems)), matched); err != nil {
			log.Printf("metadata: updating job counts: %v", err)
		}
	}

	svc.finish(job, nil)
}

// processItem looks up item against the provider for its kind and, if
// found, writes the match. Returns matchedNow=false (with a nil error) for
// a kind with no configured/enabled provider, or a provider lookup that
// legitimately found nothing -- both are normal outcomes, not failures.
func (svc *Service) processItem(ctx context.Context, item *store.MediaItem, musicEnabled, audiobookEnabled bool) (bool, error) {
	bundle := svc.currentProviders()
	switch item.Kind {
	case "movie":
		if bundle.provider == nil {
			return false, nil
		}
		year := 0
		if item.ReleaseDate != nil {
			year = item.ReleaseDate.Year()
		}
		match, err := bundle.provider.MatchMovie(ctx, item.Title, year)
		if err != nil || match == nil {
			return false, err
		}
		svc.enrich(ctx, "movie", bundle, match)
		return true, svc.applyTMDbMatch(item.ID, match)

	case "series":
		if bundle.provider == nil && bundle.fallbackSeriesProvider == nil {
			return false, nil
		}
		var match *Match
		var err error
		if bundle.provider != nil {
			match, err = bundle.provider.MatchSeries(ctx, item.Title)
			if err != nil {
				return false, err
			}
		}
		if match == nil && bundle.fallbackSeriesProvider != nil {
			match, err = bundle.fallbackSeriesProvider.MatchSeries(ctx, item.Title)
			if err != nil || match == nil {
				return false, err
			}
		}
		if match == nil {
			return false, nil
		}
		svc.enrich(ctx, "series", bundle, match)
		return true, svc.applyTMDbMatch(item.ID, match)

	case "album":
		if svc.musicProvider == nil || !musicEnabled || item.ParentID == nil {
			return false, nil
		}
		artist, err := svc.store.GetMediaItem(*item.ParentID)
		if err != nil {
			return false, err
		}
		match, err := svc.musicProvider.MatchAlbum(ctx, artist.Title, item.Title)
		if err != nil || match == nil {
			return false, err
		}
		return true, svc.applyMusicMatch(item.ID, match)

	case "book", "audiobook":
		if svc.audiobookProvider == nil || !audiobookEnabled {
			return false, nil
		}
		match, err := svc.audiobookProvider.MatchBook(ctx, item.Title, item.Author)
		if err != nil || match == nil {
			return false, err
		}
		return true, svc.applyBookMatch(item.ID, match)

	default:
		return false, nil
	}
}

// enrich layers Fanart.tv artwork and/or OMDb ratings onto an
// already-found match, in place. Best-effort: any enrichment failure
// (provider not configured, no cross-reference ID available, a transient
// API error) just leaves those fields empty rather than failing the whole
// match -- enrichment is a bonus on top of a real match, not a
// requirement for one.
func (svc *Service) enrich(ctx context.Context, kind string, bundle *providerBundle, match *Match) {
	if bundle.fanart != nil {
		var poster, backdrop, logo string
		var err error
		switch kind {
		case "movie":
			if match.ProviderID > 0 {
				poster, backdrop, logo, err = bundle.fanart.MovieArt(ctx, match.ProviderID)
			}
		case "series":
			if match.TVDbID > 0 {
				poster, backdrop, logo, err = bundle.fanart.SeriesArt(ctx, match.TVDbID)
			}
		}
		if err == nil {
			if poster != "" {
				match.PosterURL = poster
			}
			if backdrop != "" {
				match.BackdropURL = backdrop
			}
			match.LogoURL = logo
		}
	}

	if bundle.omdb != nil && match.IMDbID != "" {
		if imdbRating, rt, err := bundle.omdb.RatingsByIMDbID(ctx, match.IMDbID); err == nil {
			match.RatingIMDb = imdbRating
			match.RatingRottenTomatoes = rt
		}
	}
}

func (svc *Service) applyTMDbMatch(itemID string, match *Match) error {
	update := store.MetadataUpdate{
		Title:                match.Title,
		Overview:             match.Overview,
		PosterURL:            match.PosterURL,
		BackdropURL:          match.BackdropURL,
		TrailerURL:           match.TrailerURL,
		LogoURL:              match.LogoURL,
		RatingIMDb:           match.RatingIMDb,
		RatingRottenTomatoes: match.RatingRottenTomatoes,
		Cast:                 toStoreCast(match.Cast),
		Directors:            match.Directors,
		Similar:              toStoreSimilar(match.Similar),
		Rating:               match.Rating,
	}
	// ProviderID is 0 for a TheTVDB fallback match (no TMDb ID at all) --
	// leaving TmdbID nil there instead of writing a bogus tmdb_id=0.
	if match.ProviderID > 0 {
		update.TmdbID = &match.ProviderID
	}
	if d, err := time.Parse("2006-01-02", match.ReleaseDate); err == nil {
		update.ReleaseDate = &d
	}
	return svc.store.ApplyMetadata(itemID, update, false)
}

func toStoreCast(cast []CastMember) []store.CastMember {
	out := make([]store.CastMember, 0, len(cast))
	for _, c := range cast {
		out = append(out, store.CastMember{Name: c.Name, Character: c.Character, PhotoURL: c.PhotoURL})
	}
	return out
}

func toStoreSimilar(similar []SearchResult) []store.SimilarTitle {
	out := make([]store.SimilarTitle, 0, len(similar))
	for _, r := range similar {
		out = append(out, store.SimilarTitle{TmdbID: r.TmdbID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterURL: r.PosterURL, Rating: r.Rating})
	}
	return out
}

// backfillCastAndSimilar fetches credits/similar/rating for an item that
// already has a tmdb_id but predates this feature -- an ID-based fetch,
// skipping the title-search step ListItemsNeedingMetadata's items still
// need. A no-op if the configured provider isn't TMDb (nothing else
// populates tmdb_id) or the item somehow has no tmdb_id despite the
// caller's filter.
func (svc *Service) backfillCastAndSimilar(ctx context.Context, item *store.MediaItem) error {
	tp, ok := svc.currentProviders().provider.(*TMDbProvider)
	if !ok || item.TmdbID == nil {
		return nil
	}
	kind := "movie"
	if item.Kind == "series" {
		kind = "tv"
	}
	cast, directors, similar := tp.castAndSimilar(ctx, kind, *item.TmdbID)
	rating, _ := tp.client.rating(ctx, kind, *item.TmdbID)
	update := store.MetadataUpdate{Cast: toStoreCast(cast), Directors: directors, Similar: toStoreSimilar(similar), Rating: rating}
	return svc.store.ApplyMetadata(item.ID, update, false)
}

func (svc *Service) applyMusicMatch(itemID string, match *MusicMatch) error {
	update := store.MetadataUpdate{
		ExternalID: &match.ReleaseMBID,
		Title:      match.Title,
		PosterURL:  match.PosterURL,
	}
	if d, err := parsePartialDate(match.ReleaseDate); err == nil {
		update.ReleaseDate = &d
	}
	return svc.store.ApplyMetadata(itemID, update, false)
}

func (svc *Service) applyBookMatch(itemID string, match *BookMatch) error {
	update := store.MetadataUpdate{
		ExternalID: &match.WorkKey,
		Title:      match.Title,
		PosterURL:  match.PosterURL,
		Author:     match.Author,
	}
	if d, err := parsePartialDate(match.ReleaseDate); err == nil {
		update.ReleaseDate = &d
	}
	return svc.store.ApplyMetadata(itemID, update, false)
}

// parsePartialDate accepts full (YYYY-MM-DD), year-month (YYYY-MM), or
// year-only (YYYY) dates -- MusicBrainz and Open Library both commonly
// return partial release dates, unlike TMDb's always-full-or-empty dates.
func parsePartialDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		if d, err := time.Parse(layout, s); err == nil {
			return d, nil
		}
	}
	return time.Time{}, errUnparseableDate
}

var errUnparseableDate = errors.New("metadata: unparseable partial date")

func (svc *Service) finish(job *store.MetadataSyncJob, err error) {
	if ferr := svc.store.FinishMetadataSyncJob(job.ID, err); ferr != nil {
		log.Printf("metadata: finishing job %s: %v", job.ID, ferr)
	}
}

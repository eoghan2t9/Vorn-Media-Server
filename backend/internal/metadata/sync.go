package metadata

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// requestSpacing keeps Vorn well under TMDb's documented ~40-50 requests per
// 10 seconds per API key, without needing a full token-bucket limiter for
// what's normally a small, human-triggered batch (one library's worth of
// unmatched titles). MusicBrainz's usage policy (~1 req/sec) and Open
// Library are both comfortably under this same spacing too.
const requestSpacing = 300 * time.Millisecond

// Service matches library items against whichever external metadata
// providers are configured. Each provider is independently nil-able: a
// fresh install has none of them, a partially-configured one might only
// have TMDb, etc. -- matchItem below skips any kind whose provider is nil
// rather than erroring.
type Service struct {
	store             *store.Store
	provider          Provider
	musicProvider     MusicProvider
	audiobookProvider AudiobookProvider
}

func NewService(st *store.Store, provider Provider) *Service {
	return &Service{store: st, provider: provider}
}

// WithMusicProvider / WithAudiobookProvider attach the optional music/
// audiobook metadata providers -- separate from the constructor since
// they're gated by their own admin toggle (see IntegrationSettings) rather
// than being required for the service to exist at all like TMDb is.
func (svc *Service) WithMusicProvider(p MusicProvider) *Service {
	svc.musicProvider = p
	return svc
}

func (svc *Service) WithAudiobookProvider(p AudiobookProvider) *Service {
	svc.audiobookProvider = p
	return svc
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

	svc.finish(job, nil)
}

// processItem looks up item against the provider for its kind and, if
// found, writes the match. Returns matchedNow=false (with a nil error) for
// a kind with no configured/enabled provider, or a provider lookup that
// legitimately found nothing -- both are normal outcomes, not failures.
func (svc *Service) processItem(ctx context.Context, item *store.MediaItem, musicEnabled, audiobookEnabled bool) (bool, error) {
	switch item.Kind {
	case "movie":
		if svc.provider == nil {
			return false, nil
		}
		year := 0
		if item.ReleaseDate != nil {
			year = item.ReleaseDate.Year()
		}
		match, err := svc.provider.MatchMovie(ctx, item.Title, year)
		if err != nil || match == nil {
			return false, err
		}
		return true, svc.applyTMDbMatch(item.ID, match)

	case "series":
		if svc.provider == nil {
			return false, nil
		}
		match, err := svc.provider.MatchSeries(ctx, item.Title)
		if err != nil || match == nil {
			return false, err
		}
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

func (svc *Service) applyTMDbMatch(itemID string, match *Match) error {
	update := store.MetadataUpdate{
		TmdbID:      &match.ProviderID,
		Title:       match.Title,
		Overview:    match.Overview,
		PosterURL:   match.PosterURL,
		BackdropURL: match.BackdropURL,
		TrailerURL:  match.TrailerURL,
	}
	if d, err := time.Parse("2006-01-02", match.ReleaseDate); err == nil {
		update.ReleaseDate = &d
	}
	return svc.store.ApplyMetadata(itemID, update, false)
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

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
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

const searchTimeout = 30 * time.Second

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
}

func NewService(st *store.Store, tmdb *metadata.TMDbClient, t *torrent.Service, d *debrid.Service) *Service {
	return &Service{store: st, tmdb: tmdb, torrent: t, debrid: d}
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
				Title: seasonTitle, SeasonNumber: &seasonNum,
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
					Title: epTitle, SeasonNumber: &seasonNum, EpisodeNumber: &epNum,
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
	item, err := s.store.GetMediaItem(itemID)
	if err != nil {
		return err
	}
	switch item.AcquisitionStatus {
	case "owned", "searching", "acquiring":
		return nil // nothing to do: already fulfilled or already in flight
	}

	if err := s.store.SetMediaItemAcquisitionStatus(item.ID, "searching"); err != nil {
		return err
	}
	go s.runAcquire(item)
	return nil
}

func (s *Service) runAcquire(item *store.MediaItem) {
	ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	defer cancel()

	query, err := s.buildSearchQuery(item)
	if err != nil {
		s.fail(item.ID, err)
		return
	}

	candidates, err := s.torrent.Search(ctx, query)
	if err != nil {
		s.fail(item.ID, fmt.Errorf("searching indexers: %w", err))
		return
	}

	profile, err := s.store.GetQualityProfile(item.LibraryID)
	if err != nil {
		s.fail(item.ID, err)
		return
	}

	best, err := ScoreAndPick(candidates, profile)
	if err != nil {
		s.fail(item.ID, err)
		return
	}

	accounts, err := s.store.ListDebridAccounts()
	if err != nil {
		s.fail(item.ID, err)
		return
	}
	var account *store.DebridAccount
	for _, a := range accounts {
		if a.Enabled {
			account = a
			break
		}
	}
	if account == nil {
		s.fail(item.ID, errors.New("no enabled debrid account configured"))
		return
	}

	if err := s.store.SetMediaItemAcquisitionStatus(item.ID, "acquiring"); err != nil {
		log.Printf("acquisition: setting status on %s: %v", item.ID, err)
	}

	itemID, libraryID := item.ID, item.LibraryID
	if _, err := s.debrid.AddLink(debrid.AddLinkInput{
		AccountID:   account.ID,
		SourceRef:   best.DownloadURL,
		Name:        best.Title,
		LibraryID:   &libraryID,
		MediaItemID: &itemID,
	}); err != nil {
		s.fail(item.ID, fmt.Errorf("sending release to debrid: %w", err))
	}
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

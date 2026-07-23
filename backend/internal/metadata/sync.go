package metadata

import (
	"context"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// requestSpacing keeps Vorn well under TMDb's documented ~40-50 requests per
// 10 seconds per API key, without needing a full token-bucket limiter for
// what's normally a small, human-triggered batch (one library's worth of
// unmatched titles).
const requestSpacing = 300 * time.Millisecond

type Service struct {
	store    *store.Store
	provider Provider
}

func NewService(st *store.Store, provider Provider) *Service {
	return &Service{store: st, provider: provider}
}

// StartLibrarySync matches every not-yet-matched, not-locked top-level item
// in a library against the metadata provider, in the background.
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

		match, err := svc.matchItem(ctx, item)
		if err != nil {
			log.Printf("metadata: matching item %s (%q): %v", item.ID, item.Title, err)
			continue
		}
		if match == nil {
			continue
		}

		if err := svc.applyMatch(item.ID, match); err != nil {
			log.Printf("metadata: applying match for item %s: %v", item.ID, err)
			continue
		}
		matched++

		if err := svc.store.SetMetadataSyncJobCounts(job.ID, int64(len(items)), matched); err != nil {
			log.Printf("metadata: updating job counts: %v", err)
		}
	}

	svc.finish(job, nil)
}

func (svc *Service) matchItem(ctx context.Context, item *store.MediaItem) (*Match, error) {
	switch item.Kind {
	case "movie":
		year := 0
		if item.ReleaseDate != nil {
			year = item.ReleaseDate.Year()
		}
		return svc.provider.MatchMovie(ctx, item.Title, year)
	case "series":
		return svc.provider.MatchSeries(ctx, item.Title)
	default:
		return nil, nil
	}
}

func (svc *Service) applyMatch(itemID string, match *Match) error {
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

func (svc *Service) finish(job *store.MetadataSyncJob, err error) {
	if ferr := svc.store.FinishMetadataSyncJob(job.ID, err); ferr != nil {
		log.Printf("metadata: finishing job %s: %v", job.ID, ferr)
	}
}

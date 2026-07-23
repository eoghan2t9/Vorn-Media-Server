// Package scanner discovers video and audio files (from real disk or a
// synthetic generator), stages them in DragonflyDB, and flushes them in
// batches into Postgres as scan_files candidates for later metadata matching.
package scanner

import (
	"context"
	"log"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const (
	flushBatchSize = 500
	flushInterval  = 250 * time.Millisecond
)

type Service struct {
	store *store.Store
	queue *Queue
}

func NewService(st *store.Store, q *Queue) *Service {
	return &Service{store: st, queue: q}
}

// FlushStagingCache clears every scan-staging key in DragonflyDB. See
// Queue.FlushStaging.
func (svc *Service) FlushStagingCache(ctx context.Context) (int64, error) {
	return svc.queue.FlushStaging(ctx)
}

// StartLibraryScan kicks off a real filesystem scan of a library's folders
// in the background and returns immediately with the created job.
func (svc *Service) StartLibraryScan(library *store.Library) (*store.ScanJob, error) {
	job, err := svc.store.CreateScanJob(library.ID, "real")
	if err != nil {
		return nil, err
	}
	isMediaFile := IsVideoFile
	if library.Type == "music" || library.Type == "audiobook" {
		isMediaFile = IsAudioFile
	}
	go svc.run(job, library.Type, func(ctx context.Context, found *atomic.Int64) {
		workers := runtime.NumCPU() * 4
		WalkConcurrent(library.Folders, workers, isMediaFile, func(f DiscoveredFile) {
			found.Add(1)
			if err := svc.queue.Push(ctx, job.ID, QueuedFile{Path: f.Path, SizeBytes: f.SizeBytes, ModifiedAt: f.ModifiedAt}); err != nil {
				log.Printf("scanner: pushing discovered file: %v", err)
			}
		})
	})
	return job, nil
}

// StartSyntheticScan populates the same staging queue with count fabricated
// file records, without touching disk, so the staging→Postgres flush
// pipeline can be benchmarked independently of real filesystem I/O.
func (svc *Service) StartSyntheticScan(libraryID string, count int) (*store.ScanJob, error) {
	job, err := svc.store.CreateScanJob(libraryID, "synthetic")
	if err != nil {
		return nil, err
	}
	go svc.run(job, "movie", func(ctx context.Context, found *atomic.Int64) {
		generateSynthetic(ctx, svc.queue, job.ID, count, found)
	})
	return job, nil
}

// run drives one scan job to completion: the producer runs in the
// background while this loop periodically flushes the staging queue into
// Postgres, then drains whatever's left as fast as possible once the
// producer signals it's done.
func (svc *Service) run(job *store.ScanJob, libraryType string, produce func(ctx context.Context, found *atomic.Int64)) {
	ctx := context.Background()
	var found atomic.Int64
	var synced atomic.Int64

	producerDone := make(chan struct{})
	go func() {
		produce(ctx, &found)
		close(producerDone)
	}()

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

waitForProducer:
	for {
		select {
		case <-producerDone:
			break waitForProducer
		case <-ticker.C:
			svc.drainAvailable(ctx, job, libraryType, &synced)
			svc.updateProgress(job, &found, &synced)
		}
	}

	// Producer has finished; drain whatever it queued right at the end.
	svc.drainAvailable(ctx, job, libraryType, &synced)
	svc.updateProgress(job, &found, &synced)

	if err := svc.queue.Delete(ctx, job.ID); err != nil {
		log.Printf("scanner: cleaning up queue for job %s: %v", job.ID, err)
	}

	// Synthetic scans exist only to benchmark the staging pipeline; promoting
	// them would flood the real catalog with fabricated titles.
	if job.Kind == "real" {
		if err := promoteScanFiles(svc.store, job.LibraryID); err != nil {
			log.Printf("scanner: promoting scan files for job %s: %v", job.ID, err)
		}
	}

	if err := svc.store.FinishScanJob(job.ID, nil); err != nil {
		log.Printf("scanner: finishing job %s: %v", job.ID, err)
	}
}

// drainAvailable flushes batches until the staging queue is (momentarily) empty.
func (svc *Service) drainAvailable(ctx context.Context, job *store.ScanJob, libraryType string, synced *atomic.Int64) {
	for {
		n, err := svc.flushBatch(ctx, job, libraryType)
		if err != nil {
			log.Printf("scanner: flushing batch for job %s: %v", job.ID, err)
			return
		}
		synced.Add(int64(n))
		if n < flushBatchSize {
			return
		}
	}
}

func (svc *Service) updateProgress(job *store.ScanJob, found, synced *atomic.Int64) {
	if err := svc.store.SetScanJobCounts(job.ID, found.Load(), synced.Load()); err != nil {
		log.Printf("scanner: updating job counts: %v", err)
	}
}

func (svc *Service) flushBatch(ctx context.Context, job *store.ScanJob, libraryType string) (int, error) {
	files, err := svc.queue.PopBatch(ctx, job.ID, flushBatchSize)
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}

	isAudio := libraryType == "music" || libraryType == "audiobook"

	batch := make([]store.ScanFileInsert, 0, len(files))
	for _, f := range files {
		if isAudio {
			parsed := ParseAudioFile(f.Path, libraryType)
			insert := store.ScanFileInsert{
				LibraryID:     job.LibraryID,
				ScanJobID:     job.ID,
				Path:          f.Path,
				SizeBytes:     f.SizeBytes,
				ModifiedAt:    &f.ModifiedAt,
				GuessedKind:   parsed.Kind,
				GuessedTitle:  parsed.Title,
				GuessedArtist: parsed.Artist,
				GuessedAlbum:  parsed.Album,
			}
			if parsed.TrackNumber > 0 {
				track := parsed.TrackNumber
				insert.EpisodeNumber = &track
			}
			batch = append(batch, insert)
			continue
		}

		parsed := ParseFilename(f.Path)
		insert := store.ScanFileInsert{
			LibraryID:    job.LibraryID,
			ScanJobID:    job.ID,
			Path:         f.Path,
			SizeBytes:    f.SizeBytes,
			ModifiedAt:   &f.ModifiedAt,
			GuessedKind:  parsed.Kind,
			GuessedTitle: parsed.Title,
		}
		if parsed.Kind == "movie" && parsed.Year > 0 {
			year := parsed.Year
			insert.GuessedYear = &year
		}
		if parsed.Kind == "episode" {
			season, episode := parsed.SeasonNumber, parsed.EpisodeNumber
			insert.SeasonNumber = &season
			insert.EpisodeNumber = &episode
		}
		batch = append(batch, insert)
	}

	if err := svc.store.UpsertScanFiles(batch); err != nil {
		return 0, err
	}
	return len(files), nil
}

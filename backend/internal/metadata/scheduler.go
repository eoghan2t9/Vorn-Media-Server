package metadata

import (
	"context"
	"log"
	"time"
)

// syncInterval mirrors acquisition.MonitorScheduler's own reasoning: fine
// enough that a newly-added item's cast/crew/similar-titles metadata shows
// up without an admin needing to remember to trigger Admin > Libraries >
// Sync Metadata by hand, without hammering TMDb/OMDb/Fanart.tv on every
// tick for libraries that have nothing new to match.
const syncInterval = 15 * time.Minute

// Scheduler periodically re-runs StartLibrarySync for every library, so
// cast/crew/similar-titles metadata gets backfilled automatically as
// content is added -- via scan, on-demand acquisition, or a manual
// torrent/NZB add -- instead of requiring an admin to manually trigger
// Admin > Libraries > Sync Metadata every time. A tick over a library with
// nothing new to match is cheap: StartLibrarySync's own item selection
// (ListItemsNeedingCastSync et al.) already skips anything already matched
// or metadata-locked. Mirrors backup.Scheduler/acquisition.MonitorScheduler's
// exact shape (a single boot-started ticker goroutine).
type Scheduler struct {
	svc *Service
}

// NewScheduler is unexported-service-agnostic on purpose (svc is always
// non-nil, see Service.NewService) -- started once at boot in cmd/vornd,
// same as backup.NewScheduler, since metadataSvc itself is never recreated
// (only its providers are swapped via Reconfigure).
func NewScheduler(svc *Service) *Scheduler {
	return &Scheduler{svc: svc}
}

// Run blocks, ticking on startup and then every syncInterval, until ctx is
// cancelled. Meant to be started in its own goroutine.
func (s *Scheduler) Run(ctx context.Context) {
	s.tick()
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	if s.svc.currentProviders().provider == nil {
		return // no TMDb configured -- nothing any sync run could match anyway
	}
	libraries, err := s.svc.store.ListLibraries()
	if err != nil {
		log.Printf("metadata: scheduler: listing libraries: %v", err)
		return
	}
	for _, lib := range libraries {
		if _, err := s.svc.StartLibrarySync(lib.ID); err != nil {
			log.Printf("metadata: scheduler: starting sync for library %s: %v", lib.ID, err)
		}
	}
}

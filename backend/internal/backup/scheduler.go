package backup

import (
	"context"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// checkInterval is how often the scheduler wakes up to check whether an
// automated backup is due -- deliberately much finer-grained than any
// actual backup interval option, so enabling the feature (or shortening
// the interval) takes effect within minutes rather than needing a
// restart to be noticed.
const checkInterval = 15 * time.Minute

// Scheduler periodically runs an automated backup once BackupSettings
// (admin-configurable, re-read on every check) says one is due, then
// trims down to MaxRetained.
type Scheduler struct {
	dsn   string
	dir   string
	store *store.Store
}

func NewScheduler(dsn, dir string, st *store.Store) *Scheduler {
	return &Scheduler{dsn: dsn, dir: dir, store: st}
}

// Run blocks, checking on startup and then every checkInterval, until ctx
// is cancelled. Meant to be started in its own goroutine.
func (s *Scheduler) Run(ctx context.Context) {
	s.maybeRun(ctx)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.maybeRun(ctx)
		}
	}
}

func (s *Scheduler) maybeRun(ctx context.Context) {
	settings, err := s.store.GetBackupSettings()
	if err != nil {
		log.Printf("backup: loading settings: %v", err)
		return
	}
	if !settings.Enabled {
		return
	}

	existing, err := List(s.dir)
	if err != nil {
		log.Printf("backup: listing existing backups: %v", err)
		return
	}
	if len(existing) > 0 && time.Since(existing[0].CreatedAt) < time.Duration(settings.IntervalHours)*time.Hour {
		return
	}

	filename, err := Run(ctx, s.dsn, s.dir)
	if err != nil {
		log.Printf("backup: automated backup failed: %v", err)
		return
	}
	log.Printf("backup: automated backup created: %s", filename)
	if err := Trim(s.dir, MaxRetained); err != nil {
		log.Printf("backup: trimming old backups: %v", err)
	}
}

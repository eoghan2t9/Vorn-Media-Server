package scanner

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"
)

// generateSynthetic pushes count fabricated file records directly into the
// staging queue, without touching disk, so the staging→Postgres flush path
// can be load-tested independently of real filesystem throughput. Movie and
// episode filenames are generated in realistic-looking patterns so the
// downstream ParseFilename step exercises the same code path a real scan
// would.
func generateSynthetic(ctx context.Context, q *Queue, jobID string, count int, found *atomic.Int64) {
	now := time.Now()
	for i := 0; i < count; i++ {
		var path string
		if i%3 == 0 {
			// Every third file looks like a TV episode.
			show := i / 3
			season := (i%12 + 1)
			episode := (i%20 + 1)
			path = fmt.Sprintf("/synthetic/Series/Show %d/Season %d/Show.%d.S%02dE%02d.mkv", show, season, show, season, episode)
		} else {
			year := 1970 + (i % 55)
			path = fmt.Sprintf("/synthetic/Movies/Synthetic Movie %d (%d).mkv", i, year)
		}

		f := QueuedFile{Path: path, SizeBytes: 1_000_000_000, ModifiedAt: now}
		if err := q.Push(ctx, jobID, f); err != nil {
			log.Printf("scanner: synthetic push failed: %v", err)
			continue
		}
		found.Add(1)
	}
}

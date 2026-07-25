package debrid

import (
	"context"
	"sync"
	"time"
)

// Limiter spaces out requests to at most perMinute per minute, proactively,
// rather than firing as fast as possible and reacting to 429s after the
// fact -- both Real-Debrid and TorBox document hard per-minute caps per API
// key, and a shared account can easily blow through them if the scanner,
// admin UI, and a background resolve are all hitting the same provider.
// Exported so other packages that also talk to a rate-limited provider
// under the same account (e.g. torrent.Service's TorBox indexer search,
// nzb.Service's TorBox usenet caching) can reuse this exact primitive
// instead of each inventing their own -- as long as each holds one
// long-lived Limiter instance (not a fresh one per call/request), which is
// what actually makes the cap real across repeated attempts.
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func NewLimiter(perMinute int) *Limiter {
	return &Limiter{interval: time.Minute / time.Duration(perMinute)}
}

func (l *Limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	wait := l.next.Sub(now)
	if wait < 0 {
		wait = 0
	}
	l.next = now.Add(wait).Add(l.interval)
	l.mu.Unlock()

	if wait == 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

package debrid

import (
	"context"
	"sync"
	"time"
)

// limiter spaces out requests to at most perMinute per minute, proactively,
// rather than firing as fast as possible and reacting to 429s after the
// fact -- both Real-Debrid and TorBox document hard per-minute caps per API
// key, and a shared account can easily blow through them if the scanner,
// admin UI, and a background resolve are all hitting the same provider.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newLimiter(perMinute int) *limiter {
	return &limiter{interval: time.Minute / time.Duration(perMinute)}
}

func (l *limiter) wait(ctx context.Context) error {
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

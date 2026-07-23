package debrid

import (
	"context"
	"testing"
	"time"
)

func TestLimiter_SpacesOutRequests(t *testing.T) {
	l := newLimiter(600) // one every 100ms
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.wait(ctx); err != nil {
			t.Fatalf("wait: %v", err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 190*time.Millisecond {
		t.Fatalf("expected at least ~200ms across 3 calls at 600/min, got %v", elapsed)
	}
}

func TestLimiter_ContextCancellation(t *testing.T) {
	l := newLimiter(1) // one every 60s, so the second call must wait
	ctx := context.Background()
	if err := l.wait(ctx); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	cancelCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if err := l.wait(cancelCtx); err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
}

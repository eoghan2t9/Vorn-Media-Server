// Package logging provides an in-memory ring buffer of recent log lines,
// with live-tail subscriptions, so the admin UI can show server logs
// without shelling out to journalctl/docker logs or needing log files on
// disk at all.
package logging

import (
	"io"
	"strings"
	"sync"
)

// Buffer is an io.Writer (set via log.SetOutput) that keeps the last max
// lines in memory and fans each new line out to any live subscribers. It
// still writes through to an underlying writer (normally os.Stdout) so
// nothing about the process's own log output changes.
type Buffer struct {
	mu         sync.Mutex
	underlying io.Writer
	max        int
	lines      []string
	subs       map[chan string]struct{}
}

func NewBuffer(underlying io.Writer, max int) *Buffer {
	return &Buffer{underlying: underlying, max: max, subs: make(map[chan string]struct{})}
}

func (b *Buffer) Write(p []byte) (int, error) {
	n, err := b.underlying.Write(p)

	line := strings.TrimRight(string(p), "\n")
	if line != "" {
		b.mu.Lock()
		b.lines = append(b.lines, line)
		if len(b.lines) > b.max {
			b.lines = b.lines[len(b.lines)-b.max:]
		}
		for ch := range b.subs {
			select {
			case ch <- line:
			default:
				// Slow subscriber: drop the line rather than block logging
				// (a lagging admin UI tab must never be able to stall the
				// server's own log output).
			}
		}
		b.mu.Unlock()
	}

	return n, err
}

// Recent returns a snapshot of the buffered lines, oldest first.
func (b *Buffer) Recent() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

// Subscribe registers a channel that receives every line logged from now
// on. The returned func must be called to unregister it (typically via
// defer) once the subscriber stops reading.
func (b *Buffer) Subscribe() (<-chan string, func()) {
	ch := make(chan string, 256)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

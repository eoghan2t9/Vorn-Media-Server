package scanner

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DiscoveredFile is a video file found on disk, not yet parsed or staged.
type DiscoveredFile struct {
	Path       string
	SizeBytes  int64
	ModifiedAt time.Time
}

// dirQueue is an unbounded work queue of directories still to be read, with
// built-in fan-out termination detection. A fixed-size buffered channel would
// risk deadlock here: a single directory containing many thousands of
// subdirectories can out-fan every worker at once, and if all of them block
// trying to send into a full channel simultaneously, none is left to drain
// it. A mutex-guarded slice has no such capacity limit.
type dirQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	items   []string
	pending int
	closed  bool
}

func newDirQueue() *dirQueue {
	q := &dirQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *dirQueue) push(dir string) {
	q.mu.Lock()
	q.pending++
	q.items = append(q.items, dir)
	q.mu.Unlock()
	q.cond.Signal()
}

// popOrWait blocks until a directory is available or every previously pushed
// directory has finished processing (ok == false).
func (q *dirQueue) popOrWait() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return "", false
	}
	last := len(q.items) - 1
	dir := q.items[last]
	q.items = q.items[:last]
	return dir, true
}

// finish marks one previously pushed directory as fully processed (including
// any subdirectories it queued). Once nothing is pending, all waiting
// workers are released.
func (q *dirQueue) finish() {
	q.mu.Lock()
	q.pending--
	if q.pending == 0 {
		q.closed = true
		q.cond.Broadcast()
	}
	q.mu.Unlock()
}

// WalkConcurrent recursively walks roots, fanning directory reads out across
// workers so large libraries scan in parallel rather than one directory at a
// time. Discovered video files are sent to emit, which must be safe to call
// concurrently.
func WalkConcurrent(roots []string, workers int, emit func(DiscoveredFile)) {
	if len(roots) == 0 {
		return
	}
	if workers < 1 {
		workers = 1
	}

	q := newDirQueue()
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for {
			dir, ok := q.popOrWait()
			if !ok {
				return
			}
			entries, err := os.ReadDir(dir)
			if err == nil {
				for _, entry := range entries {
					full := filepath.Join(dir, entry.Name())
					if entry.IsDir() {
						q.push(full)
						continue
					}
					if !isVideoFile(entry.Name()) {
						continue
					}
					info, err := entry.Info()
					if err != nil {
						continue
					}
					emit(DiscoveredFile{Path: full, SizeBytes: info.Size(), ModifiedAt: info.ModTime()})
				}
			}
			q.finish()
		}
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}

	for _, root := range roots {
		q.push(root)
	}

	wg.Wait()
}

package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWalkConcurrentFindsVideoFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Movies", "Inception (2010).mkv"))
	mustWriteFile(t, filepath.Join(root, "Movies", "poster.jpg"))
	mustWriteFile(t, filepath.Join(root, "Series", "Show", "Season 1", "Show.S01E01.mp4"))
	mustWriteFile(t, filepath.Join(root, "Series", "Show", "Season 1", "Show.S01E02.mp4"))
	mustWriteFile(t, filepath.Join(root, "Series", "Show", "readme.txt"))

	var mu sync.Mutex
	var found []string
	WalkConcurrent([]string{root}, 4, IsVideoFile, func(f DiscoveredFile) {
		mu.Lock()
		found = append(found, f.Path)
		mu.Unlock()
	})

	if len(found) != 3 {
		t.Fatalf("expected 3 video files, got %d: %v", len(found), found)
	}
}

func TestWalkConcurrentFindsAudioFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Artist", "Album", "01 Track.mp3"))
	mustWriteFile(t, filepath.Join(root, "Artist", "Album", "02 Track.flac"))
	mustWriteFile(t, filepath.Join(root, "Artist", "Album", "cover.jpg"))
	mustWriteFile(t, filepath.Join(root, "Book", "Chapter 1.m4b"))

	var mu sync.Mutex
	var found []string
	WalkConcurrent([]string{root}, 4, IsAudioFile, func(f DiscoveredFile) {
		mu.Lock()
		found = append(found, f.Path)
		mu.Unlock()
	})

	if len(found) != 3 {
		t.Fatalf("expected 3 audio files, got %d: %v", len(found), found)
	}
}

func TestWalkConcurrentEmptyRoots(t *testing.T) {
	done := make(chan struct{})
	go func() {
		WalkConcurrent(nil, 4, IsVideoFile, func(DiscoveredFile) {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WalkConcurrent(nil, ...) did not return — likely hung")
	}
}

// TestWalkConcurrentHighFanOut guards against the fan-out deadlock this
// walker was specifically designed to avoid: a single directory containing
// far more subdirectories than the (former, now-removed) channel buffer
// size, processed by very few workers.
func TestWalkConcurrentHighFanOut(t *testing.T) {
	root := t.TempDir()
	const subdirs = 5000
	for i := 0; i < subdirs; i++ {
		dir := filepath.Join(root, fmt.Sprintf("title-%05d", i))
		mustWriteFile(t, filepath.Join(dir, "movie.mkv"))
	}

	var count int64
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		WalkConcurrent([]string{root}, 2, IsVideoFile, func(DiscoveredFile) {
			mu.Lock()
			count++
			mu.Unlock()
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("WalkConcurrent hung on a high-fan-out directory — possible deadlock regression")
	}

	if count != subdirs {
		t.Fatalf("expected %d files, found %d", subdirs, count)
	}
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

package nzb

import (
	"path/filepath"
	"testing"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// TestMatchKey_SizeAndExtension tests that the composite (size, extension)
// key used in matchWebDAVURLs correctly distinguishes same-size files with
// different extensions.
func TestMatchKey_SizeAndExtension(t *testing.T) {
	// Simulate the by-key map construction from matchWebDAVURLs.
	type matchKey struct {
		size int64
		ext  string
	}

	// Two NZB files: same size, different extensions.
	files := []*store.NZBFile{
		{Name: "Show.S01E01.mkv", SizeBytes: 1_000_000, WebDAVURL: ""},
		{Name: "Show.S01E01.mp4", SizeBytes: 1_000_000, WebDAVURL: ""},
		{Name: "Show.S01E02.mkv", SizeBytes: 2_000_000, WebDAVURL: ""},
	}

	byKey := make(map[matchKey]*store.NZBFile)
	for _, f := range files {
		if scanner.IsVideoFile(f.Name) && f.WebDAVURL == "" {
			byKey[matchKey{f.SizeBytes, filepath.Ext(f.Name)}] = f
		}
	}

	if len(byKey) != 3 {
		t.Fatalf("expected 3 distinct keys (size+ext), got %d", len(byKey))
	}

	// A .mkv file at 1_000_000 should match Show.S01E01.mkv, not the .mp4.
	key := matchKey{1_000_000, ".mkv"}
	if nf, ok := byKey[key]; !ok || nf.Name != "Show.S01E01.mkv" {
		t.Fatalf("expected Show.S01E01.mkv at key %+v, got %v", key, nf)
	}

	// A .mp4 file at 1_000_000 should match Show.S01E01.mp4.
	key = matchKey{1_000_000, ".mp4"}
	if nf, ok := byKey[key]; !ok || nf.Name != "Show.S01E01.mp4" {
		t.Fatalf("expected Show.S01E01.mp4 at key %+v, got %v", key, nf)
	}

	// A .mkv at 2_000_000 should match Show.S01E02.mkv.
	key = matchKey{2_000_000, ".mkv"}
	if nf, ok := byKey[key]; !ok || nf.Name != "Show.S01E02.mkv" {
		t.Fatalf("expected Show.S01E02.mkv at key %+v, got %v", key, nf)
	}
}

// TestMatchKey_NonVideoSkipped verifies non-video files are excluded.
func TestMatchKey_NonVideoSkipped(t *testing.T) {
	type matchKey struct {
		size int64
		ext  string
	}

	files := []*store.NZBFile{
		{Name: "subtitle.srt", SizeBytes: 5000, WebDAVURL: ""},
		{Name: "Show.S01E01.mkv", SizeBytes: 1_000_000, WebDAVURL: ""},
	}

	byKey := make(map[matchKey]*store.NZBFile)
	for _, f := range files {
		if scanner.IsVideoFile(f.Name) && f.WebDAVURL == "" {
			byKey[matchKey{f.SizeBytes, filepath.Ext(f.Name)}] = f
		}
	}

	if len(byKey) != 1 {
		t.Fatalf("expected only video file in map, got %d entries", len(byKey))
	}
}

// TestMatchKey_AlreadyMatchedSkipped verifies files with an existing
// WebDAVURL are skipped (already matched in a previous run).
func TestMatchKey_AlreadyMatchedSkipped(t *testing.T) {
	type matchKey struct {
		size int64
		ext  string
	}

	files := []*store.NZBFile{
		{Name: "Show.S01E01.mkv", SizeBytes: 1_000_000, WebDAVURL: "https://already/matched.mkv"},
		{Name: "Show.S01E02.mkv", SizeBytes: 2_000_000, WebDAVURL: ""},
	}

	byKey := make(map[matchKey]*store.NZBFile)
	for _, f := range files {
		if scanner.IsVideoFile(f.Name) && f.WebDAVURL == "" {
			byKey[matchKey{f.SizeBytes, filepath.Ext(f.Name)}] = f
		}
	}

	if len(byKey) != 1 {
		t.Fatalf("expected only unmatched file in map, got %d entries", len(byKey))
	}
	if nf, ok := byKey[matchKey{2_000_000, ".mkv"}]; !ok || nf.Name != "Show.S01E02.mkv" {
		t.Fatalf("expected Show.S01E02.mkv, got %v", nf)
	}
}

// TestMatchKey_DeletePreventsDuplicateMatch verifies that once a key is
// deleted from the map (simulating a successful match), a second file
// with the same key won't find it.
func TestMatchKey_DeletePreventsDuplicateMatch(t *testing.T) {
	type matchKey struct {
		size int64
		ext  string
	}

	byKey := map[matchKey]*store.NZBFile{
		{1_000_000, ".mkv"}: {Name: "Show.S01E01.mkv"},
	}

	// Simulate a match: look up, then delete.
	key := matchKey{1_000_000, ".mkv"}
	if _, ok := byKey[key]; !ok {
		t.Fatal("expected key to exist before deletion")
	}
	delete(byKey, key)

	// Same key should now be absent.
	if _, ok := byKey[key]; ok {
		t.Fatal("expected key to be gone after deletion")
	}
}

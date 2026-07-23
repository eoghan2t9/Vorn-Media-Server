package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtworkCacheKeyIsStableAndUnique(t *testing.T) {
	a := artworkCacheKey("/media/Music/Artist/Album/01.mp3", ".jpg")
	b := artworkCacheKey("/media/Music/Artist/Album/01.mp3", ".jpg")
	if a != b {
		t.Fatalf("same path produced different keys: %q vs %q", a, b)
	}
	c := artworkCacheKey("/media/Music/Artist/Album/02.mp3", ".jpg")
	if a == c {
		t.Fatalf("different paths produced the same key: %q", a)
	}
	if filepath.Ext(a) != ".jpg" {
		t.Fatalf("expected .jpg extension, got %q", a)
	}
}

func TestExtractAndCacheArtworkNoEmbeddedArt(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{artworkCacheDir: dir}

	if key := svc.extractAndCacheArtwork("testdata/tagged.mp3"); key != "" {
		t.Errorf("expected no key for a file with tags but no picture, got %q", key)
	}
	if key := svc.extractAndCacheArtwork("testdata/untagged.mp3"); key != "" {
		t.Errorf("expected no key for an untagged file, got %q", key)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected nothing written to the cache dir, found %v", entries)
	}
}

func TestExtractAndCacheArtworkMissingFile(t *testing.T) {
	svc := &Service{artworkCacheDir: t.TempDir()}
	if key := svc.extractAndCacheArtwork("testdata/does-not-exist.mp3"); key != "" {
		t.Errorf("expected no key for a missing file, got %q", key)
	}
}

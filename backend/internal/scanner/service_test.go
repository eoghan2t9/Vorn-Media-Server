package scanner

import (
	"testing"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// fakeStoreForResolveWebDAV is a minimal implementation of nzbFileBySizeLookup
// for testing resolveWebDAVName without a real database.
type fakeStoreForResolveWebDAV struct {
	// If set, FindNZBFileBySize returns this file.
	findBySizeResult *store.NZBFile
	findBySizeErr    error
	// Captures the last call args.
	lastLibraryID string
	lastSize      int64
	lastExtension string
}

func (f *fakeStoreForResolveWebDAV) FindNZBFileBySize(libraryID string, sizeBytes int64, extension string) (*store.NZBFile, error) {
	f.lastLibraryID = libraryID
	f.lastSize = sizeBytes
	f.lastExtension = extension
	if f.findBySizeErr != nil {
		return nil, f.findBySizeErr
	}
	return f.findBySizeResult, nil
}

func TestResolveWebDAVName_LocalPath(t *testing.T) {
	fake := &fakeStoreForResolveWebDAV{}
	got := resolveWebDAVName(fake, "lib-1", "/mnt/media/Movie.mkv", 1000)
	if got != "/mnt/media/Movie.mkv" {
		t.Fatalf("expected local path returned as-is, got %q", got)
	}
	// Local paths should never hit the store.
	if fake.lastSize != 0 {
		t.Fatal("expected no store call for local path")
	}
}

func TestResolveWebDAVName_ReturnsRealName(t *testing.T) {
	fake := &fakeStoreForResolveWebDAV{
		findBySizeResult: &store.NZBFile{Name: "Show.S01E01.1080p.mkv"},
	}
	got := resolveWebDAVName(fake, "lib-1", "https://webdav.torbox.app/6388E2V4n70n30H46n79J82z69G56378.mkv", 2_000_000_000)
	if got != "Show.S01E01.1080p.mkv" {
		t.Fatalf("expected real name, got %q", got)
	}
}

func TestResolveWebDAVName_NoMatchFallsBack(t *testing.T) {
	fake := &fakeStoreForResolveWebDAV{
		findBySizeErr: store.ErrNotFound,
	}
	path := "https://webdav.torbox.app/hashname.mkv"
	got := resolveWebDAVName(fake, "lib-1", path, 5000)
	if got != path {
		t.Fatalf("expected original path on no match, got %q", got)
	}
}

func TestResolveWebDAVName_EmptyNameFallsBack(t *testing.T) {
	fake := &fakeStoreForResolveWebDAV{
		findBySizeResult: &store.NZBFile{Name: ""},
	}
	path := "https://webdav.torbox.app/hashname.mkv"
	got := resolveWebDAVName(fake, "lib-1", path, 5000)
	if got != path {
		t.Fatalf("expected original path when NZB name is empty, got %q", got)
	}
}

func TestResolveWebDAVName_PassesExtension(t *testing.T) {
	fake := &fakeStoreForResolveWebDAV{
		findBySizeResult: &store.NZBFile{Name: "Show.S01E01.1080p.mkv"},
	}
	resolveWebDAVName(fake, "lib-1", "https://webdav.torbox.app/hashname.mkv", 2_000_000_000)
	if fake.lastExtension != ".mkv" {
		t.Fatalf("expected extension .mkv, got %q", fake.lastExtension)
	}
	if fake.lastLibraryID != "lib-1" {
		t.Fatalf("expected libraryID lib-1, got %q", fake.lastLibraryID)
	}
	if fake.lastSize != 2_000_000_000 {
		t.Fatalf("expected size 2000000000, got %d", fake.lastSize)
	}
}

func TestResolveWebDAVName_DifferentExtension(t *testing.T) {
	// A .mp4 path should extract .mp4 and pass it, not .mkv.
	fake := &fakeStoreForResolveWebDAV{
		findBySizeResult: &store.NZBFile{Name: "Movie.2020.mp4"},
	}
	got := resolveWebDAVName(fake, "lib-1", "https://webdav.torbox.app/hash.mp4", 1_000_000)
	if got != "Movie.2020.mp4" {
		t.Fatalf("expected Movie.2020.mp4, got %q", got)
	}
	if fake.lastExtension != ".mp4" {
		t.Fatalf("expected extension .mp4, got %q", fake.lastExtension)
	}
}

func TestResolveWebDAVName_NoExtension(t *testing.T) {
	// A path with no extension should still work (empty string passed to store).
	fake := &fakeStoreForResolveWebDAV{
		findBySizeResult: &store.NZBFile{Name: "weirdfile"},
	}
	path := "https://webdav.torbox.app/noext"
	got := resolveWebDAVName(fake, "lib-1", path, 500)
	if got != "weirdfile" {
		t.Fatalf("expected weirdfile, got %q", got)
	}
	if fake.lastExtension != "" {
		t.Fatalf("expected empty extension, got %q", fake.lastExtension)
	}
}

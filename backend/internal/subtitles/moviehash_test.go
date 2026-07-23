package subtitles

import (
	"os"
	"path/filepath"
	"testing"
)

// TestComputeMovieHash_AllZeroMinSize checks against a known-correct vector
// from OpenSubtitles' own reference Go implementation
// (github.com/opensubtitlescli/moviehash's TestSumsWithTheLeadingPadding): a
// file of exactly the minimum valid size (131072 bytes, all zero) hashes to
// "0000000000020000" -- since every data word is zero, the hash is just the
// file size (0x20000) itself, zero-padded to 16 hex digits.
func TestComputeMovieHash_AllZeroMinSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.bin")
	if err := os.WriteFile(path, make([]byte, hashMinSize), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ComputeMovieHash(path)
	if err != nil {
		t.Fatalf("ComputeMovieHash: %v", err)
	}
	if want := "0000000000020000"; got != want {
		t.Errorf("ComputeMovieHash() = %q, want %q", got, want)
	}
}

func TestComputeMovieHash_Deterministic(t *testing.T) {
	data := make([]byte, hashMinSize+1000)
	for i := range data {
		data[i] = byte(i)
	}
	path := filepath.Join(t.TempDir(), "video.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := ComputeMovieHash(path)
	if err != nil {
		t.Fatalf("ComputeMovieHash: %v", err)
	}
	second, err := ComputeMovieHash(path)
	if err != nil {
		t.Fatalf("ComputeMovieHash: %v", err)
	}
	if first != second {
		t.Errorf("hash not deterministic: %q vs %q", first, second)
	}
	if len(first) != 16 {
		t.Errorf("hash %q is not 16 hex chars", first)
	}
}

func TestComputeMovieHash_TooSmall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.bin")
	if err := os.WriteFile(path, make([]byte, hashMinSize-1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ComputeMovieHash(path); err == nil {
		t.Fatal("expected an error for a too-small file")
	}
}

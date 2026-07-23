// Package subtitles integrates with the OpenSubtitles REST API v1
// (https://opensubtitles.stoplight.io/) to search for and download
// subtitles matched to a video file by its content hash, caching downloads
// on disk so repeat requests for the same file never touch the (metered,
// low daily) download quota again.
package subtitles

import (
	"encoding/binary"
	"fmt"
	"os"
)

const (
	hashChunkSize = 65536  // 64KB
	hashMinSize   = 131072 // 2 * hashChunkSize; OpenSubtitles' documented minimum
	hashMaxSize   = 9000000000
)

// ComputeMovieHash computes OpenSubtitles' "moviehash" (aka OSHash): file
// size plus a 64-bit wraparound sum of the first and last 64KB of the file,
// each read as little-endian uint64 words. It's content-based (not
// filename-based), which is what lets Vorn cache a download keyed by this
// hash and skip the API entirely on a repeat request for the same file.
//
// Algorithm per https://opensubtitles.stoplight.io/docs/opensubtitles-api/e3750fd63a100-getting-started#calculating-moviehash-of-video-file.
func ComputeMovieHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size < hashMinSize {
		return "", fmt.Errorf("subtitles: %q is too small for a moviehash (min %d bytes)", path, hashMinSize)
	}
	if size > hashMaxSize {
		return "", fmt.Errorf("subtitles: %q is too large for a moviehash (max %d bytes)", path, hashMaxSize)
	}

	hash := uint64(size)
	buf := make([]byte, hashChunkSize)
	for _, offset := range []int64{0, size - hashChunkSize} {
		if _, err := f.ReadAt(buf, offset); err != nil {
			return "", fmt.Errorf("subtitles: reading chunk at offset %d of %q: %w", offset, path, err)
		}
		for i := 0; i+8 <= len(buf); i += 8 {
			hash += binary.LittleEndian.Uint64(buf[i : i+8])
		}
	}

	return fmt.Sprintf("%016x", hash), nil
}

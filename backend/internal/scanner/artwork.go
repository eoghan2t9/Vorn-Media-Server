package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/dhowden/tag"
)

// mimeExt maps the picture MIME types dhowden/tag actually returns to a file
// extension for the cache filename. Anything else is skipped rather than
// guessed at.
var mimeExt = map[string]string{
	"image/jpeg": ".jpg",
	"image/jpg":  ".jpg",
	"image/png":  ".png",
}

// extractAndCacheArtwork opens path, extracts its embedded cover art (if
// any), and writes it to the artwork cache keyed by a hash of path -- stable
// across rescans (same file always maps to the same cache filename) without
// needing anywhere to persist the key. Returns "" if the file has no
// embedded picture, an unsupported picture format, or can't be read at all;
// none of those are treated as errors since most audio files have no
// embedded art and that's entirely normal.
func (svc *Service) extractAndCacheArtwork(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return ""
	}
	pic := m.Picture()
	if pic == nil || len(pic.Data) == 0 {
		return ""
	}
	ext, ok := mimeExt[pic.MIMEType]
	if !ok {
		return ""
	}

	key := artworkCacheKey(path, ext)
	dest := filepath.Join(svc.artworkCacheDir, key)
	if _, err := os.Stat(dest); err == nil {
		return key // already cached from a previous scan of this file
	}
	if err := os.WriteFile(dest, pic.Data, 0o644); err != nil {
		return ""
	}
	return key
}

func artworkCacheKey(path, ext string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:16]) + ext
}

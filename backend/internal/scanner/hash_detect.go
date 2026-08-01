package scanner

import (
	"path/filepath"
	"strings"
)

// IsProbableHash reports whether name (a bare filename without directory
// components, with or without an extension) looks like a content-addressed
// or random hash rather than a real human-readable media filename. TorBox
// WebDAV serves NZB-downloaded files under names like
// "12423E4V7n1n27H57n25T9K52G842058.mp4" — 30+ mixed-case alphanumeric
// characters with no word separators. A real media filename always has at
// least one word-like token (spaces, dots-as-separators, hyphens, or short
// runs of digits only). The cutoff of 25 characters is deliberately well
// below TorBox's own ~32-character hashes and well above any reasonable
// movie title (the longest genuine title on TMDb is under 25 chars without
// spaces).
func IsProbableHash(name string) bool {
	base := filepath.Base(name)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if len(stem) < 25 {
		return false
	}
	// A real filename always has at least one of: space, dot, hyphen, or
	// underscore (common word separators in release names).
	for _, r := range stem {
		if r == ' ' || r == '.' || r == '-' || r == '_' {
			return false
		}
	}
	return true
}

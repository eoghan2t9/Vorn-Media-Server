package scanner

import "strings"

var videoExtensions = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".avi":  true,
	".mov":  true,
	".m4v":  true,
	".wmv":  true,
	".ts":   true,
	".m2ts": true,
	".webm": true,
	".flv":  true,
	".mpg":  true,
	".mpeg": true,
}

// IsVideoFile reports whether name has a file extension the scanner (and
// any other ingestion path, e.g. the torrent auto-add watcher) treats as
// playable video.
func IsVideoFile(name string) bool {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return false
	}
	return videoExtensions[strings.ToLower(name[i:])]
}

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
}

func isVideoFile(name string) bool {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return false
	}
	return videoExtensions[strings.ToLower(name[i:])]
}

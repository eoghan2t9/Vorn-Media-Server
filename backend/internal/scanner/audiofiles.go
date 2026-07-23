package scanner

import "strings"

var audioExtensions = map[string]bool{
	".mp3":  true,
	".m4a":  true,
	".m4b":  true,
	".flac": true,
	".ogg":  true,
	".opus": true,
	".wav":  true,
	".aac":  true,
	".wma":  true,
}

// IsAudioFile reports whether name has a file extension the scanner treats
// as a playable audio file (a music track or an audiobook chapter/book).
func IsAudioFile(name string) bool {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return false
	}
	return audioExtensions[strings.ToLower(name[i:])]
}

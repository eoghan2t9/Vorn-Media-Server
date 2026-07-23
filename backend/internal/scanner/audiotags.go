package scanner

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
)

// ParsedAudio is the audio equivalent of ParsedFile. Unlike video, audio
// files reliably carry title/artist/album in embedded tags (ID3/Vorbis/MP4),
// so tags are the primary source here and filename/directory heuristics are
// only a fallback for files with missing or unreadable tags.
type ParsedAudio struct {
	Kind        string // "track" (music) | "chapter" (audiobook)
	Title       string
	Artist      string
	Album       string
	TrackNumber int // 0 if unknown
}

// ParseAudioFile extracts tag metadata from an audio file for the given
// library type ("music" or "audiobook"). It never errors -- an unreadable or
// untagged file still gets usable fallback values so scanning can proceed.
func ParseAudioFile(path, libraryType string) ParsedAudio {
	kind := "track"
	if libraryType == "audiobook" {
		kind = "chapter"
	}

	base := filepath.Base(path)
	out := ParsedAudio{
		Kind:   kind,
		Title:  cleanTitle(strings.TrimSuffix(base, filepath.Ext(base))),
		Artist: "Unknown Artist",
		// Falls back to the containing directory name -- for audiobooks this
		// is promotion's grouping key (guessed_album == book title), and for
		// music it's a reasonable "Unknown Album"-equivalent that's at least
		// informative.
		Album: filepath.Base(filepath.Dir(path)),
	}

	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return out
	}

	if t := m.Title(); t != "" {
		out.Title = t
	}
	if a := m.Artist(); a != "" {
		out.Artist = a
	} else if aa := m.AlbumArtist(); aa != "" {
		out.Artist = aa
	}
	if al := m.Album(); al != "" {
		out.Album = al
	}
	if track, _ := m.Track(); track > 0 {
		out.TrackNumber = track
	}

	return out
}

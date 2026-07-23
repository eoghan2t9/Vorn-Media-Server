package scanner

import "testing"

func TestParseAudioFile(t *testing.T) {
	t.Run("music: reads tags", func(t *testing.T) {
		got := ParseAudioFile("testdata/tagged.mp3", "music")
		want := ParsedAudio{Kind: "track", Title: "Test Title", Artist: "Test Artist", Album: "Test Album", TrackNumber: 3}
		if got != want {
			t.Errorf("ParseAudioFile(tagged, music) = %+v, want %+v", got, want)
		}
	})

	t.Run("audiobook: reads tags, kind is chapter regardless of grouping", func(t *testing.T) {
		got := ParseAudioFile("testdata/tagged.mp3", "audiobook")
		if got.Kind != "chapter" {
			t.Errorf("Kind = %q, want %q", got.Kind, "chapter")
		}
		if got.Album != "Test Album" || got.Title != "Test Title" || got.TrackNumber != 3 {
			t.Errorf("got %+v, want Album/Title/TrackNumber from tags", got)
		}
	})

	t.Run("no tags: falls back to filename and directory", func(t *testing.T) {
		got := ParseAudioFile("testdata/untagged.mp3", "music")
		if got.Title != "untagged" {
			t.Errorf("Title = %q, want fallback from filename %q", got.Title, "untagged")
		}
		if got.Album != "testdata" {
			t.Errorf("Album = %q, want fallback to containing directory %q", got.Album, "testdata")
		}
		if got.Artist != "Unknown Artist" {
			t.Errorf("Artist = %q, want fallback %q", got.Artist, "Unknown Artist")
		}
		if got.TrackNumber != 0 {
			t.Errorf("TrackNumber = %d, want 0 (unknown)", got.TrackNumber)
		}
	})

	t.Run("nonexistent file: still returns usable fallback values, never errors", func(t *testing.T) {
		got := ParseAudioFile("testdata/does-not-exist.mp3", "audiobook")
		if got.Kind != "chapter" || got.Title != "does-not-exist" {
			t.Errorf("got %+v, want fallback values derived from the path alone", got)
		}
	})
}

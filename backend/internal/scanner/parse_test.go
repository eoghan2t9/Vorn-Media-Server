package scanner

import "testing"

func TestParseFilename(t *testing.T) {
	cases := []struct {
		path string
		want ParsedFile
	}{
		{
			path: "/movies/Inception (2010).mkv",
			want: ParsedFile{Kind: "movie", Title: "Inception", Year: 2010},
		},
		{
			path: "/movies/The.Matrix.1999.1080p.BluRay.x264.mkv",
			want: ParsedFile{Kind: "movie", Title: "The Matrix", Year: 1999},
		},
		{
			path: "/movies/Arrival.mp4",
			want: ParsedFile{Kind: "movie", Title: "Arrival"},
		},
		{
			path: "/series/Breaking Bad/Breaking.Bad.S01E02.720p.mkv",
			want: ParsedFile{Kind: "episode", Title: "Breaking Bad", SeasonNumber: 1, EpisodeNumber: 2},
		},
		{
			path: "/series/The Wire/The Wire - s2e05 - Title.mkv",
			want: ParsedFile{Kind: "episode", Title: "The Wire", SeasonNumber: 2, EpisodeNumber: 5},
		},
		{
			path: "/series/Old Show/Old.Show.3x10.mkv",
			want: ParsedFile{Kind: "episode", Title: "Old Show", SeasonNumber: 3, EpisodeNumber: 10},
		},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			got := ParseFilename(c.path)
			if got != c.want {
				t.Errorf("ParseFilename(%q) = %+v, want %+v", c.path, got, c.want)
			}
		})
	}
}

func TestLooksLikeSingleEpisode(t *testing.T) {
	cases := map[string]bool{
		"Breaking.Bad.S01E02.720p.mkv":       true,
		"The Wire - s2e05 - Title.mkv":       true,
		"Old.Show.3x10.mkv":                  true,
		"Breaking.Bad.S01.COMPLETE.1080p":    false,
		"Breaking.Bad.Season.1.1080p.BluRay": false,
		"Inception.2010.1080p.BluRay":        false,
	}
	for title, want := range cases {
		if got := LooksLikeSingleEpisode(title); got != want {
			t.Errorf("LooksLikeSingleEpisode(%q) = %v, want %v", title, got, want)
		}
	}
}

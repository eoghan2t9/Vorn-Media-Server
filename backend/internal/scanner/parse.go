package scanner

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type ParsedFile struct {
	Kind          string // "movie" | "episode"
	Title         string
	Year          int
	SeasonNumber  int
	EpisodeNumber int
}

var (
	// S01E02, s1e2, S01.E02
	episodeSxxExxRe = regexp.MustCompile(`(?i)[Ss](\d{1,2})[._ ]?[Ee](\d{1,3})`)
	// 1x02
	episodeNxNRe = regexp.MustCompile(`(?i)(\d{1,2})x(\d{1,3})`)
	// (2020) or .2020. or " 2020 " — a 19xx/20xx year, not part of a longer digit run.
	yearRe = regexp.MustCompile(`(?:\(|\.|_|\s)(19\d{2}|20\d{2})(?:\)|\.|_|\s|$)`)
)

// ParseFilename guesses whether a video file is a movie or an episode and
// extracts a display title (plus year, or season/episode) from its name.
// It is intentionally simple regex-based heuristics, not a full metadata
// lookup -- that happens against TMDb in a later phase.
func ParseFilename(path string) ParsedFile {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	cleaned := strings.NewReplacer(".", " ", "_", " ").Replace(base)
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	if loc := episodeSxxExxRe.FindStringSubmatchIndex(base); loc != nil {
		season, _ := strconv.Atoi(base[loc[2]:loc[3]])
		episode, _ := strconv.Atoi(base[loc[4]:loc[5]])
		title := cleanTitle(base[:loc[0]])
		return ParsedFile{Kind: "episode", Title: title, SeasonNumber: season, EpisodeNumber: episode}
	}

	if loc := episodeNxNRe.FindStringSubmatchIndex(base); loc != nil {
		season, _ := strconv.Atoi(base[loc[2]:loc[3]])
		episode, _ := strconv.Atoi(base[loc[4]:loc[5]])
		title := cleanTitle(base[:loc[0]])
		return ParsedFile{Kind: "episode", Title: title, SeasonNumber: season, EpisodeNumber: episode}
	}

	if loc := yearRe.FindStringSubmatchIndex(base); loc != nil {
		year, _ := strconv.Atoi(base[loc[2]:loc[3]])
		title := cleanTitle(base[:loc[2]-1])
		return ParsedFile{Kind: "movie", Title: title, Year: year}
	}

	return ParsedFile{Kind: "movie", Title: cleaned}
}

func cleanTitle(raw string) string {
	cleaned := strings.NewReplacer(".", " ", "_", " ").Replace(raw)
	cleaned = strings.Trim(cleaned, " -._")
	return strings.Join(strings.Fields(cleaned), " ")
}

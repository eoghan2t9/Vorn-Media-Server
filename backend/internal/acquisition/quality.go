// Package acquisition orchestrates turning a "not yet owned" TMDb title
// into a playable stream on demand: searching torrent indexers, scoring
// candidates against a library's quality profile, and handing the winner to
// a debrid provider to resolve into a direct stream URL.
package acquisition

import (
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

// Resolution is a coarse release-title resolution tag. Higher-value
// resolutions sort later in resolutionOrder.
type Resolution string

const (
	Res480p    Resolution = "480p"
	Res720p    Resolution = "720p"
	Res1080p   Resolution = "1080p"
	Res2160p   Resolution = "2160p"
	ResUnknown Resolution = ""
)

// resolutionOrder ranks resolutions worst-to-best; its index doubles as
// each resolution's tier for both the [min,max] profile filter and scoring.
var resolutionOrder = []Resolution{Res480p, Res720p, Res1080p, Res2160p}

func resolutionTier(r Resolution) (int, bool) {
	for i, v := range resolutionOrder {
		if v == r {
			return i, true
		}
	}
	return 0, false
}

var resolutionPattern = regexp.MustCompile(`(?i)\b(2160p|4k|1080p|720p|480p)\b`)

// ParseResolution extracts a resolution tag from a raw release title, e.g.
// "Movie.2020.1080p.BluRay.x264-GROUP" -> Res1080p. Returns ResUnknown if
// none of the recognized tags appear.
func ParseResolution(releaseTitle string) Resolution {
	m := resolutionPattern.FindStringSubmatch(releaseTitle)
	if m == nil {
		return ResUnknown
	}
	if strings.EqualFold(m[1], "4k") {
		return Res2160p
	}
	return Resolution(strings.ToLower(m[1]))
}

var codecPattern = regexp.MustCompile(`(?i)\b(x265|hevc|h\.?265|x264|h\.?264|av1)\b`)

// ParseCodec extracts a normalized codec tag ("x265"|"x264"|"av1") from a
// raw release title, treating HEVC/h265/h.265 as synonyms for x265 (same
// for the various h264 spellings) since indexers aren't consistent about
// which alias a given release uses.
func ParseCodec(releaseTitle string) string {
	m := codecPattern.FindStringSubmatch(releaseTitle)
	if m == nil {
		return ""
	}
	switch strings.ToLower(strings.ReplaceAll(m[1], ".", "")) {
	case "x265", "hevc", "h265":
		return "x265"
	case "x264", "h264":
		return "x264"
	case "av1":
		return "av1"
	default:
		return ""
	}
}

var remuxPattern = regexp.MustCompile(`(?i)\bremux\b`)

// IsRemux reports whether releaseTitle is tagged as a remux release.
func IsRemux(releaseTitle string) bool {
	return remuxPattern.MatchString(releaseTitle)
}

// ScoredRelease is a torrent.SearchResult annotated with what ScoreAndPick
// parsed out of its title and the score that made it (or didn't make it)
// the winner.
type ScoredRelease struct {
	torrent.SearchResult
	Resolution Resolution
	Codec      string
	Score      int
}

// ErrNoAcceptableRelease is returned by ScoreAndPick when every candidate
// was filtered out by the profile (too few seeders, or resolution outside
// [MinResolution, MaxResolution]).
var ErrNoAcceptableRelease = errors.New("acquisition: no release matched the quality profile")

// ErrNoSearchResults is distinct from ErrNoAcceptableRelease: it means the
// torrent search itself came back with zero candidates -- most commonly no
// torrent indexers are configured/enabled at all, so there was nothing for
// the quality profile to even filter. ErrNoAcceptableRelease, by contrast,
// means candidates existed but every one failed the profile (seeders/
// resolution) -- a much more specific, easier-to-act-on distinction than
// one shared "no release matched" message for both cases.
var ErrNoSearchResults = errors.New("acquisition: no torrent search results found -- check that at least one torrent indexer is configured and enabled")

const seederScoreCap = 500

// resolutionBonus rewards higher resolutions, but only among the tiers a
// profile actually allows -- a release just outside [min,max] is dropped
// entirely during filtering, never merely down-scored.
func resolutionBonus(tier int) int { return tier * 100 }

// ScoreAndRank filters candidates against profile (drops releases with
// fewer than MinSeeders, or a parsed resolution outside
// [MinResolution, MaxResolution] -- an unparseable/unknown resolution is
// kept, since a real release with an unusual title is still better than no
// release at all) and returns every survivor sorted best-first, for a
// caller that wants to try more than just the single top pick (e.g.
// falling back to the next-best candidate if the winner fails to resolve).
func ScoreAndRank(candidates []torrent.SearchResult, profile store.QualityProfile) []ScoredRelease {
	minTier, ok := resolutionTier(Resolution(profile.MinResolution))
	if !ok {
		minTier = 0
	}
	maxTier, ok := resolutionTier(Resolution(profile.MaxResolution))
	if !ok {
		maxTier = len(resolutionOrder) - 1
	}

	var ranked []ScoredRelease
	for _, c := range candidates {
		if c.Seeders < profile.MinSeeders {
			continue
		}
		res := ParseResolution(c.Title)
		if tier, known := resolutionTier(res); known && (tier < minTier || tier > maxTier) {
			continue
		}

		codec := ParseCodec(c.Title)
		score := min(c.Seeders, seederScoreCap)
		if tier, known := resolutionTier(res); known {
			score += resolutionBonus(tier)
		}
		if profile.PreferredCodec != "" && codec == profile.PreferredCodec {
			score += 50
		}
		if profile.PreferRemux && IsRemux(c.Title) {
			score += 25
		}

		ranked = append(ranked, ScoredRelease{SearchResult: c, Resolution: res, Codec: codec, Score: score})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	return ranked
}

// ScoreAndPick returns just ScoreAndRank's single highest-scoring survivor,
// for callers that only ever want the top pick (e.g. the quality-upgrade
// check, which only cares whether the current best candidate beats what's
// already owned).
func ScoreAndPick(candidates []torrent.SearchResult, profile store.QualityProfile) (*ScoredRelease, error) {
	ranked := ScoreAndRank(candidates, profile)
	if len(ranked) == 0 {
		return nil, ErrNoAcceptableRelease
	}
	return &ranked[0], nil
}

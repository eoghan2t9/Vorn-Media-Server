package acquisition

import (
	"testing"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

func TestParseResolution(t *testing.T) {
	cases := map[string]Resolution{
		"Movie.2020.1080p.BluRay.x264-GROUP": Res1080p,
		"Movie.2020.2160p.UHD.BluRay.x265":   Res2160p,
		"Movie.2020.4K.HDR.x265":             Res2160p,
		"Movie.2020.720p.WEB-DL":             Res720p,
		"Movie.2020.480p.XviD":               Res480p,
		"Movie.2020.DVDRip.XviD":             ResUnknown,
	}
	for title, want := range cases {
		if got := ParseResolution(title); got != want {
			t.Errorf("ParseResolution(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestParseCodec(t *testing.T) {
	cases := map[string]string{
		"Movie.2020.1080p.BluRay.x264-GROUP": "x264",
		"Movie.2020.1080p.BluRay.H.264":      "x264",
		"Movie.2020.2160p.HEVC-GROUP":        "x265",
		"Movie.2020.2160p.x265-GROUP":        "x265",
		"Movie.2020.AV1-GROUP":               "av1",
		"Movie.2020.BluRay":                  "",
	}
	for title, want := range cases {
		if got := ParseCodec(title); got != want {
			t.Errorf("ParseCodec(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestIsRemux(t *testing.T) {
	if !IsRemux("Movie.2020.1080p.BluRay.REMUX-GROUP") {
		t.Error("expected REMUX release to be detected")
	}
	if IsRemux("Movie.2020.1080p.BluRay.x264-GROUP") {
		t.Error("expected non-remux release not to be detected")
	}
}

func candidate(title string, seeders int) torrent.SearchResult {
	return torrent.SearchResult{Title: title, Seeders: seeders, DownloadURL: "magnet:?xt=urn:btih:" + title}
}

func TestScoreAndRank_OrdersBestFirstAndFiltersConsistentlyWithScoreAndPick(t *testing.T) {
	profile := store.QualityProfile{MinResolution: "480p", MaxResolution: "2160p", MinSeeders: 1}
	candidates := []torrent.SearchResult{
		candidate("Movie.2020.720p.WEB-DL", 50),
		candidate("Movie.2020.2160p.WEB-DL", 200),
		candidate("Movie.2020.1080p.WEB-DL", 100),
	}
	ranked := ScoreAndRank(candidates, profile)
	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked candidates, got %d", len(ranked))
	}
	for i := 1; i < len(ranked); i++ {
		if ranked[i-1].Score < ranked[i].Score {
			t.Fatalf("expected non-increasing score order, got %+v", ranked)
		}
	}
	if ranked[0].Resolution != Res2160p {
		t.Fatalf("expected 2160p to rank first, got %+v", ranked[0])
	}

	picked, err := ScoreAndPick(candidates, profile)
	if err != nil {
		t.Fatalf("ScoreAndPick: %v", err)
	}
	if picked.SearchResult.Title != ranked[0].SearchResult.Title {
		t.Fatalf("expected ScoreAndPick to match ScoreAndRank's top entry: picked=%+v ranked[0]=%+v", picked, ranked[0])
	}
}

func TestScoreAndRank_EmptyForNoQualifyingCandidates(t *testing.T) {
	profile := store.QualityProfile{MinResolution: "480p", MaxResolution: "2160p", MinSeeders: 10}
	ranked := ScoreAndRank([]torrent.SearchResult{candidate("Movie.2020.1080p.WEB-DL", 1)}, profile)
	if len(ranked) != 0 {
		t.Fatalf("expected no ranked candidates, got %+v", ranked)
	}
}

func TestScoreAndPick_PrefersHigherResolution(t *testing.T) {
	profile := store.QualityProfile{MinResolution: "480p", MaxResolution: "2160p", MinSeeders: 1}
	candidates := []torrent.SearchResult{
		candidate("Movie.2020.720p.WEB-DL", 100),
		candidate("Movie.2020.1080p.WEB-DL", 100),
	}
	got, err := ScoreAndPick(candidates, profile)
	if err != nil {
		t.Fatalf("ScoreAndPick: %v", err)
	}
	if got.Resolution != Res1080p {
		t.Fatalf("expected 1080p to win on equal seeders, got %+v", got)
	}
}

func TestScoreAndPick_FiltersOutsideResolutionBand(t *testing.T) {
	profile := store.QualityProfile{MinResolution: "1080p", MaxResolution: "1080p", MinSeeders: 1}
	candidates := []torrent.SearchResult{
		candidate("Movie.2020.2160p.WEB-DL", 500),
		candidate("Movie.2020.1080p.WEB-DL", 10),
		candidate("Movie.2020.720p.WEB-DL", 500),
	}
	got, err := ScoreAndPick(candidates, profile)
	if err != nil {
		t.Fatalf("ScoreAndPick: %v", err)
	}
	if got.Resolution != Res1080p {
		t.Fatalf("expected only the 1080p candidate to survive the [1080p,1080p] band, got %+v", got)
	}
}

func TestScoreAndPick_FiltersMinSeeders(t *testing.T) {
	profile := store.QualityProfile{MinResolution: "480p", MaxResolution: "2160p", MinSeeders: 10}
	candidates := []torrent.SearchResult{candidate("Movie.2020.1080p.WEB-DL", 5)}
	if _, err := ScoreAndPick(candidates, profile); err != ErrNoAcceptableRelease {
		t.Fatalf("expected ErrNoAcceptableRelease for below-min-seeders candidate, got %v", err)
	}
}

func TestScoreAndPick_KeepsUnknownResolution(t *testing.T) {
	profile := store.QualityProfile{MinResolution: "1080p", MaxResolution: "2160p", MinSeeders: 1}
	candidates := []torrent.SearchResult{candidate("Movie.2020.DVDRip.XviD", 50)}
	got, err := ScoreAndPick(candidates, profile)
	if err != nil {
		t.Fatalf("expected an unparseable-resolution release to still be eligible, got err: %v", err)
	}
	if got.Resolution != ResUnknown {
		t.Fatalf("unexpected resolution: %+v", got)
	}
}

func TestScoreAndPick_PreferredCodecAndRemuxBonus(t *testing.T) {
	profile := store.QualityProfile{
		MinResolution: "1080p", MaxResolution: "2160p", MinSeeders: 1,
		PreferredCodec: "x265", PreferRemux: true,
	}
	candidates := []torrent.SearchResult{
		candidate("Movie.2020.1080p.BluRay.x264-GROUP", 100),
		candidate("Movie.2020.1080p.BluRay.REMUX.x265-GROUP", 100),
	}
	got, err := ScoreAndPick(candidates, profile)
	if err != nil {
		t.Fatalf("ScoreAndPick: %v", err)
	}
	if got.Codec != "x265" || !IsRemux(got.Title) {
		t.Fatalf("expected the x265 remux release to win on bonuses, got %+v", got)
	}
}

func TestScoreAndPick_NoCandidates(t *testing.T) {
	profile := store.QualityProfile{MinResolution: "480p", MaxResolution: "2160p", MinSeeders: 1}
	if _, err := ScoreAndPick(nil, profile); err != ErrNoAcceptableRelease {
		t.Fatalf("expected ErrNoAcceptableRelease for empty candidates, got %v", err)
	}
}

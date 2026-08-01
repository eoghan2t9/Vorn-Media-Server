package transcode

import (
	"context"
	"fmt"
)

// runtimeToleranceFraction is how far a resolved release's actual duration
// may differ from its expected (TMDb) runtime before VerifyRuntime rejects
// it. 0.30 (30%) tolerates extended/director's cuts (LOTR theatrical
// 178m → extended 228m = +28%), PAL speedup (~4%), re-encodes with
// slightly different chapter boundaries, and minor TMDb imprecision,
// while still catching wildly wrong content (a 20-minute porno labeled as
// a 124-minute action movie = 84% off) that the prior 1.0 tolerance let
// through.
const runtimeToleranceFraction = 0.30

// VerifyRuntime probes path (local file or remote provider URL -- Probe
// already supports both) and compares its actual duration against
// expectedMinutes, returning an error if they differ by more than
// runtimeToleranceFraction, if the probe itself fails, or if it reports no
// usable duration. This is deliberately content-blind (duration only) --
// it exists because an indexer can serve a release whose title/filename
// claims to be one thing while the actual payload is something else
// entirely (observed in production: a completely obfuscated filename that
// resolved to unrelated content, off by 3x in duration from the requested
// movie) -- duration is intrinsic to the media itself and can't be spoofed
// by a misleading name the way a filename or subject line can.
func VerifyRuntime(ctx context.Context, path string, expectedMinutes int) error {
	if expectedMinutes <= 0 {
		return nil // no expected runtime known -- nothing to verify against
	}

	info, err := Probe(ctx, path)
	if err != nil {
		return fmt.Errorf("probing resolved media: %w", err)
	}
	if info.DurationSeconds <= 0 {
		return fmt.Errorf("probe reported no usable duration")
	}

	expected := float64(expectedMinutes) * 60
	diff := info.DurationSeconds - expected
	if diff < 0 {
		diff = -diff
	}
	if diff/expected > runtimeToleranceFraction {
		return fmt.Errorf("duration %.0fs is too far from the expected ~%d min (%.0fs)", info.DurationSeconds, expectedMinutes, expected)
	}
	return nil
}

const (
	// minMovieDurationSeconds is the shortest a promoted movie file must
	// be, regardless of whether TMDb runtime data is available. Anything
	// shorter than 10 minutes is almost certainly not a real feature film
	// and catches the common "porno/sample labeled as movie" case.
	minMovieDurationSeconds = 10 * 60
	// minEpisodeDurationSeconds is the shortest a promoted episode file
	// must be. Even the shortest legitimate TV episodes (adult swim
	// shorts, etc.) are at least a couple of minutes.
	minEpisodeDurationSeconds = 2 * 60
)

// VerifyContentDuration probes path and rejects it if the actual duration
// is below the minimum for its kind (movie or episode). Unlike
// VerifyRuntime, this check runs even when TMDb data is unavailable —
// it catches obviously wrong content (a 20-minute adult video labeled
// as a feature film, a 30-second sample labeled as an episode) on every
// promotion path, not just the on-demand-acquisition path that has TMDb
// metadata available.
func VerifyContentDuration(ctx context.Context, path, kind string) error {
	var minDuration float64
	switch kind {
	case "movie":
		minDuration = minMovieDurationSeconds
	case "episode":
		minDuration = minEpisodeDurationSeconds
	default:
		return nil // music/audiobook — no minimum-duration check
	}

	info, err := Probe(ctx, path)
	if err != nil {
		return fmt.Errorf("probing %s: %w", kind, err)
	}
	if info.DurationSeconds <= 0 {
		return fmt.Errorf("%s probe reported no usable duration", kind)
	}
	if info.DurationSeconds < minDuration {
		return fmt.Errorf("%s duration %.0fs is below the minimum %s duration of %.0fs", kind, info.DurationSeconds, kind, minDuration)
	}
	return nil
}

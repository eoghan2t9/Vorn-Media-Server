package transcode

import (
	"context"
	"fmt"
)

// runtimeToleranceFraction is how far a resolved release's actual duration
// may differ from its expected (TMDb) runtime before VerifyRuntime rejects
// it. Generous enough to tolerate extended cuts, TMDb data imprecision, and
// re-encodes that trim/pad credits, while still catching wildly wrong
// content -- the incident this guards against was off by roughly 3x.
const runtimeToleranceFraction = 0.35

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

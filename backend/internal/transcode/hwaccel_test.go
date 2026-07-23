package transcode

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestDetectBackendsSoftwareFallback exercises the real ffmpeg binary (no
// mocking -- this is exactly the "does hwaccel actually work here" probe
// Vorn runs at startup). It only asserts the software encoder, since CI/dev
// boxes vary wildly in what hardware they expose; hardware backends are
// logged for visibility but not asserted on.
func TestDetectBackendsSoftwareFallback(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed in this environment")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	backends := DetectBackends(ctx)
	t.Logf("detected backends: %+v", backends)

	found := false
	for _, b := range backends {
		if b.Name == "software" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the software (libx264) fallback to always probe successfully when ffmpeg is installed")
	}
}

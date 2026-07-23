package update

import "testing"

func TestIsDockerized(t *testing.T) {
	// This test process itself isn't running in Docker, so /.dockerenv
	// shouldn't exist -- confirms the check doesn't false-positive on a
	// bare-metal/CI runner, which matters since a false positive would
	// silently disable the whole feature there.
	if IsDockerized() {
		t.Skip("this test runner is unexpectedly containerized; skipping the negative-case assertion")
	}
}

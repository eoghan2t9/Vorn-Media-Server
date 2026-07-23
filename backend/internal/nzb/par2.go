package nzb

import (
	"context"
	"os/exec"
	"path/filepath"
)

// repairWithPar2 shells out to par2 (par2cmdline / par2cmdline-turbo) to
// verify and, if necessary, repair a completed download using .par2
// recovery files alongside it. If no par2 binary is on PATH, or the
// download has no .par2 files, repair is skipped rather than failing the
// download outright — par2 sets are optional on most Usenet uploads.
func repairWithPar2(ctx context.Context, dir string) error {
	par2Path, err := exec.LookPath("par2")
	if err != nil {
		return nil
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.par2"))
	if err != nil || len(matches) == 0 {
		return nil
	}

	cmd := exec.CommandContext(ctx, par2Path, "repair", "-q", matches[0])
	cmd.Dir = dir
	return cmd.Run()
}

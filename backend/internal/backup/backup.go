// Package backup manages automated, on-disk database backups: running
// pg_dump on a schedule, listing what's stored, and trimming old ones down
// to a fixed retention count. This is distinct from the admin-triggered
// manual download/restore in httpapi/admin_backup.go, which never touches
// disk (streamed straight to/from the HTTP request) -- these are snapshots
// kept server-side so an admin has something to fall back on even if they
// never remembered to click "download" themselves.
package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	filePrefix = "vorn-backup-auto-"
	fileSuffix = ".sql"

	// MaxRetained is how many automated backups are kept on disk -- the
	// oldest are deleted after each successful new backup.
	MaxRetained = 5
)

// autoFilenamePattern is deliberately strict (fixed prefix, exact digit
// counts, fixed suffix) since {filename} arrives from an admin-facing URL
// path param -- validating against this before ever joining it onto
// backupDir is what rules out path traversal, not filepath.Clean alone.
var autoFilenamePattern = regexp.MustCompile(`^vorn-backup-auto-\d{8}-\d{6}\.sql$`)

// ValidFilename reports whether name is exactly the shape an automated
// backup's filename takes.
func ValidFilename(name string) bool {
	return autoFilenamePattern.MatchString(name)
}

type Info struct {
	Filename  string
	SizeBytes int64
	CreatedAt time.Time
}

// List returns automated backups in dir, newest first. A missing
// directory (never backed up yet) is an empty list, not an error.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !autoFilenamePattern.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Filename: e.Name(), SizeBytes: info.Size(), CreatedAt: info.ModTime()})
	}
	// Filenames embed a sortable YYYYMMDD-HHMMSS timestamp, so lexicographic
	// order is chronological order.
	sort.Slice(out, func(i, j int) bool { return out[i].Filename > out[j].Filename })
	return out, nil
}

// Run performs a pg_dump of dsn into dir under a fresh timestamped
// filename, returning that filename. Writes to a .tmp path first and
// renames into place atomically, so a concurrent List() (or the scheduler
// racing a manual trigger) never sees a partially-written file.
func Run(ctx context.Context, dsn, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("backup: creating directory: %w", err)
	}
	filename := filePrefix + time.Now().UTC().Format("20060102-150405") + fileSuffix
	finalPath := filepath.Join(dir, filename)
	tmpPath := finalPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("backup: creating temp file: %w", err)
	}
	defer os.Remove(tmpPath) // no-op once successfully renamed below

	// Same flags as the manual download (admin_backup.go): --clean
	// --if-exists so a restore replaces what's there instead of erroring
	// on every table already existing.
	cmd := exec.CommandContext(ctx, "pg_dump", "--no-owner", "--no-privileges", "--clean", "--if-exists", dsn)
	cmd.Stdout = f
	runErr := cmd.Run()
	closeErr := f.Close()
	if runErr != nil {
		return "", fmt.Errorf("backup: pg_dump: %w", runErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("backup: writing dump file: %w", closeErr)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("backup: finalizing dump file: %w", err)
	}
	return filename, nil
}

// Trim deletes automated backups in dir beyond the newest keep.
func Trim(dir string, keep int) error {
	backups, err := List(dir)
	if err != nil {
		return err
	}
	if len(backups) <= keep {
		return nil
	}
	for _, b := range backups[keep:] {
		if err := os.Remove(filepath.Join(dir, b.Filename)); err != nil {
			return fmt.Errorf("backup: removing old backup %s: %w", b.Filename, err)
		}
	}
	return nil
}

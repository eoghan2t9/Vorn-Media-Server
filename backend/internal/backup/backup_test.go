package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidFilename(t *testing.T) {
	valid := []string{"vorn-backup-auto-20260724-050133.sql"}
	invalid := []string{
		"vorn-backup-auto-20260724-050133.sql.tmp",
		"../vorn-backup-auto-20260724-050133.sql",
		"vorn-backup-auto-2026-07-24.sql",
		"vorn-backup-20260724-050133.sql", // manual download's filename shape, not auto's
		"",
	}
	for _, name := range valid {
		if !ValidFilename(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}
	for _, name := range invalid {
		if ValidFilename(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func touchBackup(t *testing.T, dir, name string, mtime time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("-- dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestList_MissingDirIsEmptyNotError(t *testing.T) {
	backups, err := List(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no backups, got %d", len(backups))
	}
}

func TestList_SortsNewestFirstAndIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().UTC()
	touchBackup(t, dir, "vorn-backup-auto-20260101-000000.sql", base.AddDate(0, 0, -2))
	touchBackup(t, dir, "vorn-backup-auto-20260103-000000.sql", base)
	touchBackup(t, dir, "vorn-backup-auto-20260102-000000.sql", base.AddDate(0, 0, -1))
	if err := os.WriteFile(filepath.Join(dir, "vorn-backup-auto-20260104-000000.sql.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "some-other-file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	backups, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups (ignoring .tmp and unrelated files), got %d: %+v", len(backups), backups)
	}
	want := []string{
		"vorn-backup-auto-20260103-000000.sql",
		"vorn-backup-auto-20260102-000000.sql",
		"vorn-backup-auto-20260101-000000.sql",
	}
	for i, name := range want {
		if backups[i].Filename != name {
			t.Errorf("position %d: expected %q, got %q", i, name, backups[i].Filename)
		}
	}
}

func TestTrim_KeepsOnlyNewest(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().UTC()
	names := []string{
		"vorn-backup-auto-20260101-000000.sql",
		"vorn-backup-auto-20260102-000000.sql",
		"vorn-backup-auto-20260103-000000.sql",
		"vorn-backup-auto-20260104-000000.sql",
		"vorn-backup-auto-20260105-000000.sql",
		"vorn-backup-auto-20260106-000000.sql",
		"vorn-backup-auto-20260107-000000.sql",
	}
	for i, name := range names {
		touchBackup(t, dir, name, base.AddDate(0, 0, i))
	}

	if err := Trim(dir, MaxRetained); err != nil {
		t.Fatalf("Trim: %v", err)
	}

	remaining, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(remaining) != MaxRetained {
		t.Fatalf("expected %d remaining, got %d", MaxRetained, len(remaining))
	}
	// The 5 newest (...03 through ...07) should survive; ...01 and ...02 removed.
	for _, gone := range []string{"vorn-backup-auto-20260101-000000.sql", "vorn-backup-auto-20260102-000000.sql"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("expected %s to have been removed", gone)
		}
	}
}

func TestTrim_FewerThanKeepIsNoop(t *testing.T) {
	dir := t.TempDir()
	touchBackup(t, dir, "vorn-backup-auto-20260101-000000.sql", time.Now())
	if err := Trim(dir, MaxRetained); err != nil {
		t.Fatalf("Trim: %v", err)
	}
	remaining, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected the single backup to survive, got %d", len(remaining))
	}
}

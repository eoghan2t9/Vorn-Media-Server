package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	os.Unsetenv("VORN_TEST_ENVFILE_A")
	os.Unsetenv("VORN_TEST_ENVFILE_B")
	t.Setenv("VORN_TEST_ENVFILE_B", "already-set-value")

	path := filepath.Join(t.TempDir(), "vornd.env")
	content := "# a comment\n\nVORN_TEST_ENVFILE_A=from-file\nVORN_TEST_ENVFILE_B=should-not-override\nmalformed line without equals\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile: %v", err)
	}

	if got := os.Getenv("VORN_TEST_ENVFILE_A"); got != "from-file" {
		t.Errorf("VORN_TEST_ENVFILE_A = %q, want from-file", got)
	}
	if got := os.Getenv("VORN_TEST_ENVFILE_B"); got != "already-set-value" {
		t.Errorf("VORN_TEST_ENVFILE_B = %q, want already-set-value (real env must win)", got)
	}
}

func TestLoadEnvFile_MissingFile(t *testing.T) {
	if err := LoadEnvFile("/nonexistent/path/vornd.env"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

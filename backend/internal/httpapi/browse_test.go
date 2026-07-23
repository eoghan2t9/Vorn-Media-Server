package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleBrowseFilesystem(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "Movies"))
	mustMkdir(t, filepath.Join(root, "TV Shows"))
	mustMkdir(t, filepath.Join(root, ".hidden"))
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{}

	t.Run("lists visible directories only, sorted, skips files and dotdirs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/browse?path="+root, nil)
		w := httptest.NewRecorder()
		s.handleBrowseFilesystem(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var resp browseResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Directories) != 2 {
			t.Fatalf("directories = %+v, want 2 entries (Movies, TV Shows)", resp.Directories)
		}
		if resp.Directories[0].Name != "Movies" || resp.Directories[1].Name != "TV Shows" {
			t.Fatalf("unexpected order/contents: %+v", resp.Directories)
		}
		if resp.Parent == "" {
			t.Fatalf("expected a non-empty parent for a non-root path")
		}
	})

	t.Run("navigating into a subdirectory returns its own listing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/browse?path="+filepath.Join(root, "Movies"), nil)
		w := httptest.NewRecorder()
		s.handleBrowseFilesystem(w, req)

		var resp browseResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Directories) != 0 {
			t.Fatalf("expected an empty Movies dir, got %+v", resp.Directories)
		}
		if resp.Parent != root {
			t.Fatalf("parent = %q, want %q", resp.Parent, root)
		}
	})

	t.Run("relative path is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/browse?path=relative/path", nil)
		w := httptest.NewRecorder()
		s.handleBrowseFilesystem(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for a relative path", w.Code)
		}
	})

	t.Run("nonexistent path is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/browse?path="+filepath.Join(root, "nope"), nil)
		w := httptest.NewRecorder()
		s.handleBrowseFilesystem(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for a missing directory", w.Code)
		}
	})

	t.Run("root filesystem entry has no parent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/browse?path=/", nil)
		w := httptest.NewRecorder()
		s.handleBrowseFilesystem(w, req)
		var resp browseResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Parent != "" {
			t.Fatalf("parent = %q, want empty at filesystem root", resp.Parent)
		}
	})
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

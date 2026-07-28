package debrid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// premiumizeFake simulates directdl returning nothing cached (forcing the
// transfer/create -> poll -> folder/list fallback path), so both code
// paths in Resolve get exercised.
type premiumizeFake struct {
	polls int
}

func (f *premiumizeFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/transfer/directdl":
			json.NewEncoder(w).Encode(pmDirectDLResponse{pmStatus: pmStatus{Status: "success"}})
		case r.URL.Path == "/transfer/create":
			json.NewEncoder(w).Encode(pmCreateTransferResponse{pmStatus: pmStatus{Status: "success"}, ID: "t1"})
		case r.URL.Path == "/transfer/list":
			f.polls++
			status := "running"
			if f.polls >= 2 {
				status = "finished"
			}
			json.NewEncoder(w).Encode(pmTransferListResponse{
				pmStatus: pmStatus{Status: "success"},
				Transfers: []struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					Message  string `json:"message"`
					FolderID string `json:"folder_id"`
					FileID   string `json:"file_id"`
				}{{ID: "t1", Status: status, FolderID: "f1"}},
			})
		case r.URL.Path == "/folder/list":
			json.NewEncoder(w).Encode(pmFolderListResponse{
				pmStatus: pmStatus{Status: "success"},
				Content: []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					Type       string `json:"type"`
					Size       int64  `json:"size"`
					Link       string `json:"link"`
					StreamLink string `json:"stream_link"`
				}{{ID: "1", Name: "Movie.2020.mkv", Type: "file", Size: 4000, Link: "https://premiumize.example/FAKE1"}},
			})
		case r.URL.Path == "/account/info":
			until := int64(1893456000)
			json.NewEncoder(w).Encode(pmAccountInfoResponse{
				pmStatus:     pmStatus{Status: "success"},
				CustomerID:   123,
				PremiumUntil: &until,
			})
		default:
			http.NotFound(w, r)
		}
	}
}

func TestPremiumizeClient_Resolve_FallsBackToTransfer(t *testing.T) {
	fake := &premiumizeFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewPremiumizeClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)
	c.pollInterval = time.Millisecond

	result, err := c.Resolve(context.Background(), "test-key", "deadbeef")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.ProviderRef != "t1" {
		t.Fatalf("expected provider ref %q, got %q", "t1", result.ProviderRef)
	}
	if len(result.Files) != 1 || result.Files[0].Name != "Movie.2020.mkv" || result.Files[0].StreamURL != "https://premiumize.example/FAKE1" {
		t.Fatalf("unexpected resolved files: %+v", result.Files)
	}
	if fake.polls < 2 {
		t.Fatalf("expected waitForTransfer to poll more than once, polled %d times", fake.polls)
	}
}

// TestPremiumizeClient_FolderFiles_Recurses guards against the real bug
// where a non-recursive folder listing silently dropped every file inside a
// nested subfolder (common for season packs / extras).
func TestPremiumizeClient_FolderFiles_Recurses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		switch id {
		case "root":
			json.NewEncoder(w).Encode(pmFolderListResponse{
				pmStatus: pmStatus{Status: "success"},
				Content: []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					Type       string `json:"type"`
					Size       int64  `json:"size"`
					Link       string `json:"link"`
					StreamLink string `json:"stream_link"`
				}{
					{ID: "sub", Name: "Season 01", Type: "folder"},
					{ID: "f-top", Name: "Show.S01E01.mkv", Type: "file", Size: 1000, Link: "https://premiumize.example/E01", StreamLink: "https://premiumize.example/E01-stream"},
				},
			})
		case "sub":
			json.NewEncoder(w).Encode(pmFolderListResponse{
				pmStatus: pmStatus{Status: "success"},
				Content: []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					Type       string `json:"type"`
					Size       int64  `json:"size"`
					Link       string `json:"link"`
					StreamLink string `json:"stream_link"`
				}{{ID: "f-nested", Name: "Show.S01E02.mkv", Type: "file", Size: 2000, Link: "https://premiumize.example/E02"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewPremiumizeClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)

	files, err := c.folderFiles(context.Background(), "test-key", "root", "")
	if err != nil {
		t.Fatalf("folderFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files (1 top-level + 1 nested), got %d: %+v", len(files), files)
	}
	byName := map[string]ResolvedFile{}
	for _, f := range files {
		byName[f.Name] = f
	}
	if f, ok := byName["Show.S01E01.mkv"]; !ok || f.StreamURL != "https://premiumize.example/E01-stream" {
		t.Fatalf("expected top-level file to prefer stream_link, got %+v", f)
	}
	if f, ok := byName["Season 01/Show.S01E02.mkv"]; !ok || f.StreamURL != "https://premiumize.example/E02" {
		t.Fatalf("expected nested file with joined path, got files: %+v", files)
	}
}

func TestPremiumizeClient_Delete(t *testing.T) {
	var deletedID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/transfer/delete" {
			r.ParseForm()
			deletedID = r.FormValue("id")
			json.NewEncoder(w).Encode(pmStatus{Status: "success"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewPremiumizeClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)

	if err := c.Delete(context.Background(), "test-key", "t1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deletedID != "t1" {
		t.Fatalf("expected delete for t1, got %q", deletedID)
	}
	if err := c.Delete(context.Background(), "test-key", ""); err != nil {
		t.Fatalf("Delete with empty providerRef should be a no-op, got: %v", err)
	}
}

func TestPremiumizeClient_AccountInfo(t *testing.T) {
	fake := &premiumizeFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewPremiumizeClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)

	info, err := c.AccountInfo(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("AccountInfo: %v", err)
	}
	if !info.Premium || info.Username != "customer #123" {
		t.Fatalf("unexpected account info: %+v", info)
	}
}

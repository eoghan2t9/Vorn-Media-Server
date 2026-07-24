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
					ID   string `json:"id"`
					Name string `json:"name"`
					Type string `json:"type"`
					Size int64  `json:"size"`
					Link string `json:"link"`
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
	c.limiter = newLimiter(1_000_000)
	c.pollInterval = time.Millisecond

	files, err := c.Resolve(context.Background(), "test-key", "deadbeef")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(files) != 1 || files[0].Name != "Movie.2020.mkv" || files[0].StreamURL != "https://premiumize.example/FAKE1" {
		t.Fatalf("unexpected resolved files: %+v", files)
	}
	if fake.polls < 2 {
		t.Fatalf("expected waitForTransfer to poll more than once, polled %d times", fake.polls)
	}
}

func TestPremiumizeClient_AccountInfo(t *testing.T) {
	fake := &premiumizeFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewPremiumizeClient()
	c.baseURL = srv.URL
	c.limiter = newLimiter(1_000_000)

	info, err := c.AccountInfo(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("AccountInfo: %v", err)
	}
	if !info.Premium || info.Username != "customer #123" {
		t.Fatalf("unexpected account info: %+v", info)
	}
}

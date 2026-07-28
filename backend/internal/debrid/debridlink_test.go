package debrid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// debridLinkFake simulates the Debrid-Link endpoints DebridLinkClient
// calls, reaching downloadPercent 100 only after a couple of polls.
type debridLinkFake struct {
	polls int
}

func (f *debridLinkFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/seedbox/add":
			json.NewEncoder(w).Encode(dlEnvelope[dlTorrent]{
				Success: true,
				Value:   dlTorrent{ID: "abc", Status: 1},
			})
		case r.URL.Path == "/seedbox/list":
			f.polls++
			pct := 60
			var files []dlFile
			if f.polls >= 2 {
				pct = 100
				files = []dlFile{{ID: "abc-1", Name: "Movie.2020.mkv", Size: 5000, DownloadURL: "https://seed.debrid-link.example/FAKE1", DownloadPercent: 100}}
			}
			json.NewEncoder(w).Encode(dlEnvelope[[]dlTorrent]{
				Success: true,
				Value:   []dlTorrent{{ID: "abc", Status: 4, DownloadPercent: pct, Files: files}},
			})
		case r.URL.Path == "/account/infos":
			json.NewEncoder(w).Encode(dlEnvelope[dlAccountInfo]{
				Success: true,
				Value:   dlAccountInfo{Username: "amy", AccountType: 1, PremiumLeft: 3628800},
			})
		default:
			http.NotFound(w, r)
		}
	}
}

func TestDebridLinkClient_Resolve(t *testing.T) {
	fake := &debridLinkFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewDebridLinkClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)
	c.pollInterval = time.Millisecond

	result, err := c.Resolve(context.Background(), "test-key", "deadbeef")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.ProviderRef != "abc" {
		t.Fatalf("expected provider ref %q, got %q", "abc", result.ProviderRef)
	}
	if len(result.Files) != 1 || result.Files[0].Name != "Movie.2020.mkv" || result.Files[0].StreamURL != "https://seed.debrid-link.example/FAKE1" {
		t.Fatalf("unexpected resolved files: %+v", result.Files)
	}
	if fake.polls < 2 {
		t.Fatalf("expected waitUntilReady to poll more than once, polled %d times", fake.polls)
	}
}

func TestDebridLinkClient_Delete(t *testing.T) {
	var deletedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/remove") {
			deletedPath = r.URL.Path
			json.NewEncoder(w).Encode(dlEnvelope[json.RawMessage]{Success: true})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewDebridLinkClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)

	if err := c.Delete(context.Background(), "test-key", "abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deletedPath != "/seedbox/abc/remove" {
		t.Fatalf("expected DELETE /seedbox/abc/remove, got %q", deletedPath)
	}
	if err := c.Delete(context.Background(), "test-key", ""); err != nil {
		t.Fatalf("Delete with empty providerRef should be a no-op, got: %v", err)
	}
}

func TestDebridLinkClient_AccountInfo(t *testing.T) {
	fake := &debridLinkFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewDebridLinkClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)

	info, err := c.AccountInfo(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("AccountInfo: %v", err)
	}
	if !info.Premium || info.Username != "amy" {
		t.Fatalf("unexpected account info: %+v", info)
	}
}

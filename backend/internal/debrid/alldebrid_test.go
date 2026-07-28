package debrid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// allDebridFake simulates the AllDebrid endpoints AllDebridClient calls,
// reaching statusCode 4 ("Ready") only after a couple of status polls, so
// waitUntilReady actually has to poll.
type allDebridFake struct {
	polls int
}

func (f *allDebridFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v4/magnet/upload":
			json.NewEncoder(w).Encode(adEnvelope[adMagnetUploadData]{
				Status: "success",
				Data: adMagnetUploadData{Magnets: []struct {
					ID    int         `json:"id"`
					Ready bool        `json:"ready"`
					Error *adAPIError `json:"error,omitempty"`
				}{{ID: 99}}},
			})
		case "/v4.1/magnet/status":
			f.polls++
			code := 2
			status := "Downloading"
			if f.polls >= 2 {
				code = 4
				status = "Ready"
			}
			json.NewEncoder(w).Encode(adEnvelope[adMagnetStatusData]{
				Status: "success",
				Data: adMagnetStatusData{Magnets: []struct {
					ID         int    `json:"id"`
					Status     string `json:"status"`
					StatusCode int    `json:"statusCode"`
				}{{ID: 99, Status: status, StatusCode: code}}},
			})
		case "/v4/magnet/files":
			json.NewEncoder(w).Encode(adEnvelope[adMagnetFilesData]{
				Status: "success",
				Data: adMagnetFilesData{Magnets: []struct {
					ID    int           `json:"id"`
					Files []adFileEntry `json:"files"`
				}{{ID: 99, Files: []adFileEntry{
					{N: "Movie.2020.mkv", S: 3000, L: "https://alldebrid.com/f/FAKE1"},
					{N: "Extras", E: []adFileEntry{
						{N: "sample.mkv", S: 100, L: "https://alldebrid.com/f/FAKE2"},
					}},
				}}}},
			})
		case "/v4/user":
			json.NewEncoder(w).Encode(adEnvelope[adUserData]{
				Status: "success",
				Data: adUserData{User: struct {
					Username     string `json:"username"`
					IsPremium    bool   `json:"isPremium"`
					PremiumUntil int64  `json:"premiumUntil"`
				}{Username: "amy", IsPremium: true, PremiumUntil: 1893456000}},
			})
		default:
			http.NotFound(w, r)
		}
	}
}

func TestAllDebridClient_Resolve(t *testing.T) {
	fake := &allDebridFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewAllDebridClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)
	c.pollInterval = time.Millisecond

	result, err := c.Resolve(context.Background(), "test-key", "deadbeef")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.ProviderRef != "99" {
		t.Fatalf("expected provider ref %q, got %q", "99", result.ProviderRef)
	}
	if len(result.Files) != 2 {
		t.Fatalf("expected 2 flattened files (including nested folder), got %d", len(result.Files))
	}
	if fake.polls < 2 {
		t.Fatalf("expected waitUntilReady to poll more than once, polled %d times", fake.polls)
	}
}

// TestAllDebridClient_Resolve_SkipsPollWhenReady guards against a needless
// round trip when the upload response already reports the magnet as ready
// (the common case for a popular, already-cached torrent) -- waitUntilReady
// (and therefore /v4.1/magnet/status) must never be called in that case.
func TestAllDebridClient_Resolve_SkipsPollWhenReady(t *testing.T) {
	statusCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4/magnet/upload":
			json.NewEncoder(w).Encode(adEnvelope[adMagnetUploadData]{
				Status: "success",
				Data: adMagnetUploadData{Magnets: []struct {
					ID    int         `json:"id"`
					Ready bool        `json:"ready"`
					Error *adAPIError `json:"error,omitempty"`
				}{{ID: 7, Ready: true}}},
			})
		case "/v4.1/magnet/status":
			statusCalled = true
			http.Error(w, "should not be called", http.StatusInternalServerError)
		case "/v4/magnet/files":
			json.NewEncoder(w).Encode(adEnvelope[adMagnetFilesData]{
				Status: "success",
				Data: adMagnetFilesData{Magnets: []struct {
					ID    int           `json:"id"`
					Files []adFileEntry `json:"files"`
				}{{ID: 7, Files: []adFileEntry{{N: "Movie.2020.mkv", S: 3000, L: "https://alldebrid.com/f/FAKE1"}}}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewAllDebridClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)

	if _, err := c.Resolve(context.Background(), "test-key", "deadbeef"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if statusCalled {
		t.Fatal("expected waitUntilReady to be skipped when the upload response already reports ready=true")
	}
}

func TestAllDebridClient_Delete(t *testing.T) {
	var deletedID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4/magnet/delete" {
			r.ParseForm()
			deletedID = r.FormValue("id")
			json.NewEncoder(w).Encode(adEnvelope[json.RawMessage]{Status: "success"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewAllDebridClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)

	if err := c.Delete(context.Background(), "test-key", "99"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deletedID != "99" {
		t.Fatalf("expected delete for id 99, got %q", deletedID)
	}
	if err := c.Delete(context.Background(), "test-key", ""); err != nil {
		t.Fatalf("Delete with empty providerRef should be a no-op, got: %v", err)
	}
}

func TestAllDebridClient_AccountInfo(t *testing.T) {
	fake := &allDebridFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewAllDebridClient()
	c.baseURL = srv.URL
	c.limiter = NewLimiter(1_000_000)

	info, err := c.AccountInfo(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("AccountInfo: %v", err)
	}
	if info.Username != "amy" || !info.Premium {
		t.Fatalf("unexpected account info: %+v", info)
	}
}

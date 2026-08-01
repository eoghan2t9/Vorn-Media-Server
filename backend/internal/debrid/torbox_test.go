package debrid

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"context"
)

// torBoxFake simulates the TorBox endpoints TorBoxClient calls, becoming
// "download_finished" only after createtorrent has run, so waitForCache
// actually has to poll.
type torBoxFake struct {
	polls       int
	usenetPolls int
}

func (f *torBoxFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/torrents/createtorrent"):
			mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
				http.Error(w, "expected multipart body", http.StatusBadRequest)
				return
			}
			mr := multipart.NewReader(r.Body, params["boundary"])
			var magnet string
			for {
				part, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if part.FormName() == "magnet" {
					data, _ := io.ReadAll(part)
					magnet = string(data)
				}
			}
			if magnet == "" {
				http.Error(w, "missing magnet field", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(tbEnvelope[tbCreateTorrentData]{
				Success: true,
				Data:    tbCreateTorrentData{TorrentID: 42},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/torrents/mylist"):
			f.polls++
			json.NewEncoder(w).Encode(tbEnvelope[[]tbTorrentInfo]{
				Success: true,
				Data: []tbTorrentInfo{{
					ID:               42,
					DownloadFinished: f.polls >= 2,
					Files: []TBFile{
						{ID: 7, Name: "Movie.2020.mkv", Size: 2000},
					},
				}},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/torrents/requestdl"):
			if r.URL.Query().Get("token") != "test-key" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(tbEnvelope[string]{
				Success: true,
				Data:    "https://torbox-cdn.example/FAKE/Movie.2020.mkv",
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/usenet/createusenetdownload"):
			mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
				http.Error(w, "expected multipart body", http.StatusBadRequest)
				return
			}
			mr := multipart.NewReader(r.Body, params["boundary"])
			var sawFile bool
			for {
				part, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if part.FormName() == "file" {
					data, _ := io.ReadAll(part)
					sawFile = len(data) > 0
				}
			}
			if !sawFile {
				http.Error(w, "missing file field", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(tbEnvelope[tbCreateUsenetData]{
				Success: true,
				Data:    tbCreateUsenetData{UsenetDownloadID: 99, Hash: "ABC123DEF456"},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/usenet/mylist"):
			f.usenetPolls++
			json.NewEncoder(w).Encode(tbEnvelope[[]TBUsenetInfo]{
				Success: true,
				Data: []TBUsenetInfo{{
					ID:               99,
					DownloadFinished: f.usenetPolls >= 2,
					DownloadPresent:  f.usenetPolls >= 2,
					Progress:         float64(f.usenetPolls) / 2,
					Files: []TBFile{
						{ID: 3, Name: "Show.S01E01.mkv", Size: 1500},
					},
				}},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/usenet/requestdl"):
			if r.URL.Query().Get("token") != "test-key" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(tbEnvelope[string]{
				Success: true,
				Data:    "https://torbox-cdn.example/FAKE/Show.S01E01.mkv",
			})
		default:
			http.NotFound(w, r)
		}
	}
}

func TestTorBoxClient_Resolve(t *testing.T) {
	fake := &torBoxFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewTorBoxClient(NewLimiter(1_000_000))
	c.baseURL = srv.URL
	c.pollInterval = time.Millisecond

	result, err := c.Resolve(context.Background(), "test-key", "deadbeef")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.ProviderRef != "42" {
		t.Fatalf("expected provider ref %q, got %q", "42", result.ProviderRef)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	got := result.Files[0]
	if got.Name != "Movie.2020.mkv" || got.SizeBytes != 2000 || got.StreamURL != "https://torbox-cdn.example/FAKE/Movie.2020.mkv" {
		t.Fatalf("unexpected resolved file: %+v", got)
	}
	if fake.polls < 2 {
		t.Fatalf("expected waitForCache to poll more than once, polled %d times", fake.polls)
	}
}

func TestTorBoxClient_Delete(t *testing.T) {
	var deletedID, deletedOp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/torrents/controltorrent" {
			r.ParseMultipartForm(1 << 20)
			deletedID = r.FormValue("torrent_id")
			deletedOp = r.FormValue("operation")
			json.NewEncoder(w).Encode(tbEnvelope[json.RawMessage]{Success: true})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewTorBoxClient(NewLimiter(1_000_000))
	c.baseURL = srv.URL

	if err := c.Delete(context.Background(), "test-key", "42"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deletedID != "42" || deletedOp != "delete" {
		t.Fatalf("expected torrent_id=42 operation=delete, got id=%q op=%q", deletedID, deletedOp)
	}
	if err := c.Delete(context.Background(), "test-key", ""); err != nil {
		t.Fatalf("Delete with empty providerRef should be a no-op, got: %v", err)
	}
}

func TestTorBoxClient_DeleteUsenetDownload(t *testing.T) {
	var deletedID, deletedOp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/usenet/controlusenetdownload" {
			r.ParseMultipartForm(1 << 20)
			deletedID = r.FormValue("usenet_id")
			deletedOp = r.FormValue("operation")
			json.NewEncoder(w).Encode(tbEnvelope[json.RawMessage]{Success: true})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewTorBoxClient(NewLimiter(1_000_000))
	c.baseURL = srv.URL

	if err := c.DeleteUsenetDownload(context.Background(), "test-key", 99); err != nil {
		t.Fatalf("DeleteUsenetDownload: %v", err)
	}
	if deletedID != "99" || deletedOp != "delete" {
		t.Fatalf("expected usenet_id=99 operation=delete, got id=%q op=%q", deletedID, deletedOp)
	}
	if err := c.DeleteUsenetDownload(context.Background(), "test-key", 0); err != nil {
		t.Fatalf("DeleteUsenetDownload with id 0 should be a no-op, got: %v", err)
	}
}

// TestTorBoxClient_WaitForUsenetCache_FailsFast guards against burning the
// full poll timeout when TorBox already knows the download failed --
// download_state carrying "failed"/"invalid" must abort immediately.
func TestTorBoxClient_WaitForUsenetCache_FailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/usenet/mylist") {
			json.NewEncoder(w).Encode(tbEnvelope[[]TBUsenetInfo]{
				Success: true,
				Data:    []TBUsenetInfo{{ID: 5, DownloadState: "failed: missing articles"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewTorBoxClient(NewLimiter(1_000_000))
	c.baseURL = srv.URL
	c.pollInterval = time.Millisecond

	start := time.Now()
	_, err := c.WaitForUsenetCache(context.Background(), "test-key", 5, nil)
	if err == nil {
		t.Fatal("expected an error for a failed usenet download, got nil")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("expected fast failure, took %v", elapsed)
	}
}

func TestTorBoxClient_Usenet(t *testing.T) {
	fake := &torBoxFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewTorBoxClient(NewLimiter(1_000_000))
	c.baseURL = srv.URL
	c.pollInterval = time.Millisecond

	ctx := context.Background()
	usenetID, webdavHash, err := c.CreateUsenetDownload(ctx, "test-key", []byte("<nzb/>"), "Show.S01E01")
	if err != nil {
		t.Fatalf("CreateUsenetDownload: %v", err)
	}
	if usenetID != 99 {
		t.Fatalf("expected usenet id 99, got %d", usenetID)
	}
	if webdavHash == "" {
		t.Fatalf("expected non-empty webdav hash")
	}

	var lastProgress float64
	files, err := c.WaitForUsenetCache(ctx, "test-key", usenetID, func(p float64) { lastProgress = p })
	if err != nil {
		t.Fatalf("WaitForUsenetCache: %v", err)
	}
	if len(files) != 1 || files[0].Name != "Show.S01E01.mkv" || files[0].Size != 1500 {
		t.Fatalf("unexpected files: %+v", files)
	}
	if lastProgress != 1 {
		t.Fatalf("expected final progress 1, got %v", lastProgress)
	}
	if fake.usenetPolls < 2 {
		t.Fatalf("expected WaitForUsenetCache to poll more than once, polled %d times", fake.usenetPolls)
	}

	link, err := c.RequestUsenetDownloadLink(ctx, "test-key", usenetID, files[0].ID)
	if err != nil {
		t.Fatalf("RequestUsenetDownloadLink: %v", err)
	}
	if link != "https://torbox-cdn.example/FAKE/Show.S01E01.mkv" {
		t.Fatalf("unexpected link: %s", link)
	}
}

// TestTorBoxClient_UsenetInfo_BareObjectShape guards against a real
// production regression: /usenet/mylist?id=X was observed returning data
// as a bare JSON object rather than a single-element array, unlike
// /torrents/mylist's always-an-array shape. usenetInfo must handle both.
func TestTorBoxClient_UsenetInfo_BareObjectShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"id":42,"download_finished":true,"download_present":true,"progress":1,"files":[{"id":7,"name":"Movie.mkv","size":2000}]}}`))
	}))
	defer srv.Close()

	c := NewTorBoxClient(NewLimiter(1_000_000))
	c.baseURL = srv.URL

	item, err := c.usenetInfo(context.Background(), "test-key", 42)
	if err != nil {
		t.Fatalf("usenetInfo: %v", err)
	}
	if item == nil || item.ID != 42 || !item.DownloadFinished || !item.DownloadPresent {
		t.Fatalf("unexpected item: %+v", item)
	}
	if len(item.Files) != 1 || item.Files[0].Name != "Movie.mkv" {
		t.Fatalf("unexpected files: %+v", item.Files)
	}
}

func TestTorBoxClient_CheckCached(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(tbEnvelope[[]tbCachedInfo]{
			Success: true,
			Data:    []tbCachedInfo{{Hash: "AABBCC"}},
		})
	}))
	defer srv.Close()

	c := NewTorBoxClient(NewLimiter(1_000_000))
	c.baseURL = srv.URL

	result, err := c.CheckCached(context.Background(), "test-key", []string{"aabbcc", "ddeeff"})
	if err != nil {
		t.Fatalf("CheckCached: %v", err)
	}
	if gotPath != "/torrents/checkcached" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if !strings.Contains(gotQuery, "hash=aabbcc%2Cddeeff") {
		t.Fatalf("expected comma-joined hash query param, got %q", gotQuery)
	}
	if !result["aabbcc"] {
		t.Fatalf("expected aabbcc to be reported cached (case-insensitive), got %+v", result)
	}
	if result["ddeeff"] {
		t.Fatalf("expected ddeeff to be reported not-cached, got %+v", result)
	}
}

func TestTorBoxClient_CheckCachedUsenet(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(tbEnvelope[[]tbCachedInfo]{
			Success: true,
			Data:    []tbCachedInfo{{Hash: "urlhash1"}},
		})
	}))
	defer srv.Close()

	c := NewTorBoxClient(NewLimiter(1_000_000))
	c.baseURL = srv.URL

	result, err := c.CheckCachedUsenet(context.Background(), "test-key", []string{"urlhash1", "urlhash2"})
	if err != nil {
		t.Fatalf("CheckCachedUsenet: %v", err)
	}
	if gotPath != "/usenet/checkcached" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if !result["urlhash1"] || result["urlhash2"] {
		t.Fatalf("unexpected result: %+v", result)
	}
}

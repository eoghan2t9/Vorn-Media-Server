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
					Files: []tbFile{
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
				Data:    tbCreateUsenetData{UsenetID: 99},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/usenet/mylist"):
			f.usenetPolls++
			json.NewEncoder(w).Encode(tbEnvelope[[]tbUsenetInfo]{
				Success: true,
				Data: []tbUsenetInfo{{
					ID:               99,
					DownloadFinished: f.usenetPolls >= 2,
					DownloadPresent:  f.usenetPolls >= 2,
					Progress:         float64(f.usenetPolls) / 2,
					Files: []tbFile{
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

	c := NewTorBoxClient()
	c.baseURL = srv.URL
	c.limiter = newLimiter(1_000_000)
	c.pollInterval = time.Millisecond

	files, err := c.Resolve(context.Background(), "test-key", "deadbeef")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	got := files[0]
	if got.Name != "Movie.2020.mkv" || got.SizeBytes != 2000 || got.StreamURL != "https://torbox-cdn.example/FAKE/Movie.2020.mkv" {
		t.Fatalf("unexpected resolved file: %+v", got)
	}
	if fake.polls < 2 {
		t.Fatalf("expected waitForCache to poll more than once, polled %d times", fake.polls)
	}
}

func TestTorBoxClient_Usenet(t *testing.T) {
	fake := &torBoxFake{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := NewTorBoxClient()
	c.baseURL = srv.URL
	c.limiter = newLimiter(1_000_000)
	c.pollInterval = time.Millisecond

	ctx := context.Background()
	usenetID, err := c.CreateUsenetDownload(ctx, "test-key", []byte("<nzb/>"), "Show.S01E01")
	if err != nil {
		t.Fatalf("CreateUsenetDownload: %v", err)
	}
	if usenetID != 99 {
		t.Fatalf("expected usenet id 99, got %d", usenetID)
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

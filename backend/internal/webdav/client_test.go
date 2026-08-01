package webdav

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWalk_FindsVideoFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			http.Error(w, "expected PROPFIND", http.StatusMethodNotAllowed)
			return
		}
		// Verify Basic auth was sent.
		user, pass, ok := r.BasicAuth()
		if !ok || user != "torbox" || pass != "test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var ms multistatus
		if strings.HasSuffix(r.URL.Path, "/Movies/") {
			ms = multistatus{
				Responses: []response{
					{
						Href: "/Movies/",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop:   prop{ResourceType: resourceType{Collection: &struct{}{}}, DisplayName: "Movies"},
						},
					},
					{
						Href: "/Movies/Fight.Club.1999.mkv",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop: prop{
								ContentLength: int64Ptr(2_000_000_000),
								LastModified:  "Mon, 01 Jan 2024 12:00:00 GMT",
								DisplayName:   "Fight Club (1999).mkv",
							},
						},
					},
					{
						Href: "/Movies/Subs/",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop:   prop{ResourceType: resourceType{Collection: &struct{}{}}, DisplayName: "Subs"},
						},
					},
				},
			}
		} else if strings.HasSuffix(r.URL.Path, "/Subs/") {
			ms = multistatus{
				Responses: []response{
					{
						Href: "/Movies/Subs/",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop:   prop{ResourceType: resourceType{Collection: &struct{}{}}, DisplayName: "Subs"},
						},
					},
					{
						Href: "/Movies/Subs/subtitle.srt",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop: prop{
								ContentLength: int64Ptr(5000),
								DisplayName:   "subtitle.srt",
							},
						},
					},
				},
			}
		} else {
			// Root directory.
			ms = multistatus{
				Responses: []response{
					{
						Href: "/",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop:   prop{ResourceType: resourceType{Collection: &struct{}{}}, DisplayName: "/"},
						},
					},
					{
						Href: "/Movies/",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop:   prop{ResourceType: resourceType{Collection: &struct{}{}}, DisplayName: "Movies"},
						},
					},
					{
						Href: "/README.txt",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop: prop{
								ContentLength: int64Ptr(100),
								DisplayName:   "README.txt",
							},
						},
					},
				},
			}
		}
		w.WriteHeader(http.StatusMultiStatus)
		xml.NewEncoder(w).Encode(ms)
	}))
	defer srv.Close()

	var files []DiscoveredFile
	err := Walk(context.Background(), srv.URL+"/", "test-key", func(f DiscoveredFile) {
		files = append(files, f)
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 video file, got %d: %+v", len(files), files)
	}
	got := files[0]
	if !strings.Contains(got.Path, "Fight.Club.1999.mkv") {
		t.Fatalf("expected Fight.Club.1999.mkv in path, got %s", got.Path)
	}
	if got.SizeBytes != 2_000_000_000 {
		t.Fatalf("expected size 2_000_000_000, got %d", got.SizeBytes)
	}
	// README.txt should not have been emitted (not a video extension).
}

func TestWalk_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := Walk(context.Background(), srv.URL+"/", "wrong-key", func(f DiscoveredFile) {})
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("expected unauthorized error, got: %v", err)
	}
}

func TestWalk_EmptyDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			http.Error(w, "expected PROPFIND", http.StatusMethodNotAllowed)
			return
		}
		var ms multistatus
		if strings.HasSuffix(r.URL.Path, "/empty/") {
			ms = multistatus{
				Responses: []response{
					{
						Href: "/empty/",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop:   prop{ResourceType: resourceType{Collection: &struct{}{}}, DisplayName: "empty"},
						},
					},
				},
			}
		} else {
			ms = multistatus{
				Responses: []response{
					{
						Href: "/",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop:   prop{ResourceType: resourceType{Collection: &struct{}{}}, DisplayName: "/"},
						},
					},
					{
						Href: "/empty/",
						Propstat: propstat{
							Status: "HTTP/1.1 200 OK",
							Prop:   prop{ResourceType: resourceType{Collection: &struct{}{}}, DisplayName: "empty"},
						},
					},
				},
			}
		}
		w.WriteHeader(http.StatusMultiStatus)
		xml.NewEncoder(w).Encode(ms)
	}))
	defer srv.Close()

	var files []DiscoveredFile
	err := Walk(context.Background(), srv.URL+"/", "test-key", func(f DiscoveredFile) {
		files = append(files, f)
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files in empty dir, got %d", len(files))
	}
}

func TestRefresh_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		user, pass, ok := r.BasicAuth()
		if !ok || user != "torbox" || pass != "test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := Refresh(context.Background(), srv.URL+"/", "test-key"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if gotPath != "/refresh/" {
		t.Fatalf("expected /refresh/, got %s", gotPath)
	}
}

func TestIsMediaFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Movie.2020.mkv", true},
		{"Show.S01E01.mp4", true},
		{"track.flac", true},
		{"song.mp3", true},
		{"readme.txt", false},
		{"subtitle.srt", false},
		{"image.jpg", false},
		{".hidden.mkv", true},
		{"Movie.2020.MKV", true},
	}
	for _, tt := range tests {
		if got := isMediaFile(tt.name); got != tt.want {
			t.Errorf("isMediaFile(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestResolveURL(t *testing.T) {
	tests := []struct {
		base, href, want string
	}{
		{"https://webdav.torbox.app", "/Movies/Foo.mkv", "https://webdav.torbox.app/Movies/Foo.mkv"},
		{"https://webdav.torbox.app/", "/Movies/Foo.mkv", "https://webdav.torbox.app/Movies/Foo.mkv"},
		{"https://webdav.torbox.app/Movies", "Foo.mkv", "https://webdav.torbox.app/Movies/Foo.mkv"},
		{"https://webdav.torbox.app", "https://other.example/file.mkv", "https://other.example/file.mkv"},
	}
	for _, tt := range tests {
		if got := resolveURL(tt.base, tt.href); got != tt.want {
			t.Errorf("resolveURL(%q, %q) = %q, want %q", tt.base, tt.href, got, tt.want)
		}
	}
}

func TestPropfind_ParsesLastModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ms := multistatus{
			Responses: []response{
				{
					Href: "/file.mkv",
					Propstat: propstat{
						Status: "HTTP/1.1 200 OK",
						Prop: prop{
							ContentLength: int64Ptr(1000),
							LastModified:  "Mon, 15 Jan 2024 08:30:00 GMT",
							DisplayName:   "file.mkv",
						},
					},
				},
			},
		}
		w.WriteHeader(http.StatusMultiStatus)
		xml.NewEncoder(w).Encode(ms)
	}))
	defer srv.Close()

	entries, err := propfind(context.Background(), srv.URL+"/", "test-key")
	if err != nil {
		t.Fatalf("propfind: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	expected := time.Date(2024, time.January, 15, 8, 30, 0, 0, time.UTC)
	if !entries[0].lastModified.Equal(expected) {
		t.Fatalf("expected lastModified %v, got %v", expected, entries[0].lastModified)
	}
}

func int64Ptr(v int64) *int64 { return &v }

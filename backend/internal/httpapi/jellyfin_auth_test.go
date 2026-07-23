package httpapi

import (
	"net/http"
	"net/url"
	"testing"
)

func TestJellyfinToken(t *testing.T) {
	tests := []struct {
		name    string
		build   func(r *http.Request)
		wantTok string
	}{
		{
			name:    "X-Emby-Token header",
			build:   func(r *http.Request) { r.Header.Set("X-Emby-Token", "abc123") },
			wantTok: "abc123",
		},
		{
			name:    "X-MediaBrowser-Token header",
			build:   func(r *http.Request) { r.Header.Set("X-MediaBrowser-Token", "def456") },
			wantTok: "def456",
		},
		{
			name: "api_key query param",
			build: func(r *http.Request) {
				q := url.Values{"api_key": {"ghi789"}}
				r.URL.RawQuery = q.Encode()
			},
			wantTok: "ghi789",
		},
		{
			name: "MediaBrowser Authorization header",
			build: func(r *http.Request) {
				r.Header.Set("Authorization", `MediaBrowser Client="test", Device="test", DeviceId="1", Token="jkl012"`)
			},
			wantTok: "jkl012",
		},
		{
			name: "X-Emby-Authorization header",
			build: func(r *http.Request) {
				r.Header.Set("X-Emby-Authorization", `MediaBrowser Token="mno345"`)
			},
			wantTok: "mno345",
		},
		{
			name:    "no token present",
			build:   func(r *http.Request) {},
			wantTok: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "http://example.com/Items", nil)
			if err != nil {
				t.Fatal(err)
			}
			tt.build(r)
			if got := jellyfinToken(r); got != tt.wantTok {
				t.Errorf("jellyfinToken() = %q, want %q", got, tt.wantTok)
			}
		})
	}
}

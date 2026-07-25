package httpapi

import "testing"

func TestCanonicalizeJfPath(t *testing.T) {
	templates := []string{
		"/System/Info/Public",
		"/Users/AuthenticateByName",
		"/Users/{userId}/Views",
		"/Items/{id}",
		"/Items/{id}/Images/{type}",
		"/Videos/{id}/{filename}",
		"/emby/System/Info/Public",
		"/emby/Users/AuthenticateByName",
		"/emby/Users/{userId}/Views",
		"/emby/Items/{id}",
		"/emby/Items/{id}/Images/{type}",
		"/emby/Videos/{id}/{filename}",
	}

	tests := []struct {
		name      string
		reqPath   string
		wantPath  string
		wantMatch bool
	}{
		{
			name:      "fully lowercased static path under /emby (the official Emby app's behavior)",
			reqPath:   "/emby/system/info/public",
			wantPath:  "/emby/System/Info/Public",
			wantMatch: true,
		},
		{
			name:      "already correctly cased",
			reqPath:   "/emby/System/Info/Public",
			wantPath:  "/emby/System/Info/Public",
			wantMatch: true,
		},
		{
			name:      "lowercased static segments around a dynamic id segment",
			reqPath:   "/emby/users/abc-123-DEF/views",
			wantPath:  "/emby/Users/abc-123-DEF/Views",
			wantMatch: true,
		},
		{
			name:      "dynamic segment's own case/content is preserved verbatim, not touched",
			reqPath:   "/emby/items/MixedCaseItemID123",
			wantPath:  "/emby/Items/MixedCaseItemID123",
			wantMatch: true,
		},
		{
			name:      "two dynamic segments both preserved",
			reqPath:   "/emby/videos/SomeId/My Movie.mkv",
			wantPath:  "/emby/Videos/SomeId/My Movie.mkv",
			wantMatch: true,
		},
		{
			name:      "bare (non-/emby) lowercased path -- the real jellyfin-web client's actual behavior",
			reqPath:   "/Users/authenticatebyname",
			wantPath:  "/Users/AuthenticateByName",
			wantMatch: true,
		},
		{
			name:      "bare path already correctly cased",
			reqPath:   "/System/Info/Public",
			wantPath:  "/System/Info/Public",
			wantMatch: true,
		},
		{
			name:      "vorn's own /api paths are never touched",
			reqPath:   "/api/items/some-id",
			wantPath:  "/api/items/some-id",
			wantMatch: false,
		},
		{
			name:      "wrong segment count doesn't match anything",
			reqPath:   "/emby/system/info/public/extra",
			wantPath:  "/emby/system/info/public/extra",
			wantMatch: false,
		},
		{
			name:      "unknown route doesn't match",
			reqPath:   "/emby/totally/unknown/route",
			wantPath:  "/emby/totally/unknown/route",
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := canonicalizeJfPath(tc.reqPath, templates)
			if ok != tc.wantMatch {
				t.Errorf("canonicalizeJfPath(%q) ok = %v, want %v", tc.reqPath, ok, tc.wantMatch)
			}
			if got != tc.wantPath {
				t.Errorf("canonicalizeJfPath(%q) = %q, want %q", tc.reqPath, got, tc.wantPath)
			}
		})
	}
}

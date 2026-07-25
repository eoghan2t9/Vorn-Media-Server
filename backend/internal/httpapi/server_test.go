package httpapi

import "testing"

func TestCanonicalizeEmbyPath(t *testing.T) {
	templates := []string{
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
			name:      "fully lowercased static path (the actual official Emby app's behavior)",
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
			got, ok := canonicalizeEmbyPath(tc.reqPath, templates)
			if ok != tc.wantMatch {
				t.Errorf("canonicalizeEmbyPath(%q) ok = %v, want %v", tc.reqPath, ok, tc.wantMatch)
			}
			if got != tc.wantPath {
				t.Errorf("canonicalizeEmbyPath(%q) = %q, want %q", tc.reqPath, got, tc.wantPath)
			}
		})
	}
}

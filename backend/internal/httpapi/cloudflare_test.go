package httpapi

import (
	"net/http"
	"testing"
)

func TestRealClientIP(t *testing.T) {
	orig := cfRanges
	cfRanges = &cloudflareIPRanges{ranges: parseCIDRs([]string{"1.2.3.0/24"})}
	defer func() { cfRanges = orig }()

	tests := []struct {
		name            string
		remoteAddr      string
		cfHeader        string
		trustCloudflare bool
		want            string
	}{
		{
			name:            "trust disabled: header ignored even from a trusted-looking peer",
			remoteAddr:      "1.2.3.4:5555",
			cfHeader:        "9.9.9.9",
			trustCloudflare: false,
			want:            "1.2.3.4",
		},
		{
			name:            "trust enabled, peer in Cloudflare range: header wins",
			remoteAddr:      "1.2.3.4:5555",
			cfHeader:        "9.9.9.9",
			trustCloudflare: true,
			want:            "9.9.9.9",
		},
		{
			name:            "trust enabled, peer NOT in Cloudflare range: header ignored (can't be spoofed)",
			remoteAddr:      "8.8.8.8:5555",
			cfHeader:        "9.9.9.9",
			trustCloudflare: true,
			want:            "8.8.8.8",
		},
		{
			name:            "trust enabled, in-range peer, no header sent: falls back to peer",
			remoteAddr:      "1.2.3.4:5555",
			cfHeader:        "",
			trustCloudflare: true,
			want:            "1.2.3.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
			if err != nil {
				t.Fatal(err)
			}
			r.RemoteAddr = tt.remoteAddr
			if tt.cfHeader != "" {
				r.Header.Set("CF-Connecting-IP", tt.cfHeader)
			}
			if got := realClientIP(r, tt.trustCloudflare); got != tt.want {
				t.Errorf("realClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

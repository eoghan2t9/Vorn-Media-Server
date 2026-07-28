package acquisition

import "testing"

func TestMagnetHash(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "hex btih magnet",
			url:  "magnet:?xt=urn:btih:AABBCCDDEEFF00112233445566778899AABBCCDD&dn=Movie",
			want: "aabbccddeeff00112233445566778899aabbccdd",
		},
		{
			name: "lowercase already",
			url:  "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd",
			want: "aabbccddeeff00112233445566778899aabbccdd",
		},
		{
			name: "torrent file url has no extractable hash",
			url:  "https://indexer.example/download/12345.torrent",
			want: "",
		},
		{
			name: "empty string",
			url:  "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := magnetHash(c.url); got != c.want {
				t.Fatalf("magnetHash(%q) = %q, want %q", c.url, got, c.want)
			}
		})
	}
}

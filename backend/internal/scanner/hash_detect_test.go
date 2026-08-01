package scanner

import "testing"

func TestIsProbableHash(t *testing.T) {
	tests := []struct {
		name     string
		wantHash bool
	}{
		// Real TorBox hashes — should be detected.
		{"12423E4V7n1n27H57n25T9K52G842058.mp4", true},
		{"12423E4V7n1n27H57n25T9K52G842058", true},
		{"310248g6n90Y05Y8U5Y1z8J4J9276325.mp4", true},
		{"39828P95g36q5q50M51q6h4z93J24747.mkv", true},

		// Real filenames — should NOT be detected.
		{"xXx.2002.1080p.ALL4.WEB-DL.AAC.2.0.H.264-PiRaTeS.mkv", false},
		{"The.Matrix.1999.1080p.BluRay.x264.mkv", false},
		{"Breaking Bad S01E01.mkv", false},
		{"Inception (2010).mp4", false},
		{"some_movie_2020.mp4", false},
		{"1x02.mkv", false},

		// Short alphanumeric — not a hash.
		{"abc123.mp4", false},
		{"movie.mp4", false},
		{"12345.mkv", false},

		// Edge: long but has separators — not a hash.
		{"The.Really.Long.Movie.Title.That.Goes.On.Forever.2024.1080p.mkv", false},

		// Edge: long alphanumeric but < 25 chars — not a hash.
		{"123456789012345678901234.mp4", false}, // 24 chars
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsProbableHash(tt.name)
			if got != tt.wantHash {
				t.Errorf("IsProbableHash(%q) = %v, want %v", tt.name, got, tt.wantHash)
			}
		})
	}
}

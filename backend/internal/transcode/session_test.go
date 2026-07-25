package transcode

import (
	"strings"
	"testing"
)

func containsSeq(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j, s := range seq {
			if args[i+j] != s {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestBuildFFmpegArgs(t *testing.T) {
	sess := &Session{ID: "sess1", SourcePath: "/media/movie.mkv", OutputDir: "/tmp/out/sess1"}
	backend := Backend{Name: "software", Encoder: "libx264", ExtraArgs: []string{"-preset", "veryfast"}}

	t.Run("default audio track sends no explicit -map", func(t *testing.T) {
		args := buildFFmpegArgs(sess, backend, false, -1)
		if strings.Join(args, " ") == "" || containsSeq(args, "-map") {
			t.Errorf("buildFFmpegArgs with audioTrackIndex=-1 should not include -map, got %v", args)
		}
		if !containsSeq(args, "-c:v", "libx264") {
			t.Errorf("expected the backend's encoder, got %v", args)
		}
	})

	t.Run("explicit audio track maps both video and the chosen audio stream", func(t *testing.T) {
		args := buildFFmpegArgs(sess, backend, false, 2)
		if !containsSeq(args, "-map", "0:v:0") {
			t.Errorf("expected explicit video map alongside an explicit audio map, got %v", args)
		}
		if !containsSeq(args, "-map", "0:a:2") {
			t.Errorf("expected -map 0:a:2 for audioTrackIndex=2, got %v", args)
		}
	})

	t.Run("copyVideo skips the encoder and device/filter/extra args", func(t *testing.T) {
		args := buildFFmpegArgs(sess, backend, true, -1)
		if !containsSeq(args, "-c:v", "copy") {
			t.Errorf("expected -c:v copy when copyVideo=true, got %v", args)
		}
		if containsSeq(args, "-preset") {
			t.Errorf("preset/extra args are only relevant when actually encoding video, got %v", args)
		}
	})

	t.Run("copyVideo with an explicit audio track still maps video explicitly", func(t *testing.T) {
		args := buildFFmpegArgs(sess, backend, true, 1)
		if !containsSeq(args, "-map", "0:v:0") || !containsSeq(args, "-map", "0:a:1") || !containsSeq(args, "-c:v", "copy") {
			t.Errorf("expected explicit maps plus stream-copied video, got %v", args)
		}
	})

	t.Run("always disables subtitle streams regardless of mode", func(t *testing.T) {
		for _, tc := range []struct {
			name            string
			copyVideo       bool
			audioTrackIndex int
		}{
			{"default remux", true, -1},
			{"default transcode", false, -1},
			{"explicit audio track, remux", true, 3},
			{"explicit audio track, transcode", false, 3},
		} {
			args := buildFFmpegArgs(sess, backend, tc.copyVideo, tc.audioTrackIndex)
			if !containsSeq(args, "-sn") {
				t.Errorf("%s: expected -sn to always be present (source subtitle tracks otherwise get auto-muxed into an unserved VTT rendition), got %v", tc.name, args)
			}
		}
	})
}

package subtitles

import (
	"strings"
	"testing"
)

func TestSRTToVTT(t *testing.T) {
	srt := "1\n00:00:01,000 --> 00:00:02,500\nHello there\n\n2\n00:00:03,100 --> 00:00:04,000\nSecond line\n"
	got := string(SRTToVTT([]byte(srt)))

	if !strings.HasPrefix(got, "WEBVTT\n\n") {
		t.Fatalf("expected a WEBVTT header, got: %q", got)
	}
	if strings.Contains(got, ",000") || strings.Contains(got, ",500") || strings.Contains(got, ",100") {
		t.Errorf("expected comma timestamp separators to be converted to periods: %q", got)
	}
	if !strings.Contains(got, "00:00:01.000 --> 00:00:02.500") {
		t.Errorf("expected converted timestamp, got: %q", got)
	}
	if !strings.Contains(got, "Hello there") || !strings.Contains(got, "Second line") {
		t.Errorf("expected cue text to survive conversion: %q", got)
	}
}

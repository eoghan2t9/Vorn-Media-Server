package subtitles

import "regexp"

var srtTimestampRe = regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})`)

// SRTToVTT converts SRT subtitle content (what OpenSubtitles serves) into
// WebVTT, which is what HTML5's <track> element -- and therefore Vorn's own
// web player -- requires: a "WEBVTT" header, and "." instead of "," as the
// timestamp's millisecond separator. This assumes UTF-8 input; some SRT
// files in the wild use legacy encodings (e.g. Windows-1252), which isn't
// handled here.
func SRTToVTT(srt []byte) []byte {
	converted := srtTimestampRe.ReplaceAll(srt, []byte("$1.$2"))
	out := make([]byte, 0, len(converted)+len("WEBVTT\n\n"))
	out = append(out, "WEBVTT\n\n"...)
	out = append(out, converted...)
	return out
}

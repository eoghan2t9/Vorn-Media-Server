package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type MediaInfo struct {
	DurationSeconds float64
	VideoCodec      string
	AudioCodec      string
	Width           int
	Height          int
	// AudioTracks lists every audio stream in the file, in the order ffprobe
	// reports them -- AudioTrackInfo.Index is that position (the N in
	// ffmpeg's "-map 0:a:N"), not ffprobe's global stream index, since that's
	// what session.go's audioTrackIndex param needs to select a specific
	// track.
	AudioTracks []AudioTrackInfo
	// Chapters is empty for files with no embedded chapter markers, which is
	// most TV/movie rips -- the frontend's skip-intro button only appears
	// when a chapter's title actually looks like an intro/recap.
	Chapters []ChapterInfo
	// ContainerNative reports whether the source's own container can be
	// played directly by a browser <video> element -- distinct from codec
	// compatibility. No mainstream browser can demux Matroska (.mkv), by far
	// the most common scene/usenet release container, regardless of how
	// compatible the codecs inside it are; Decide uses this so a
	// codec-compatible .mkv doesn't get wrongly sent as ModeDirect (a 302
	// straight to a URL the browser can't parse at all) instead of the fast
	// container-only remux that case actually needs. See isNativeContainer.
	ContainerNative bool
}

type AudioTrackInfo struct {
	Index    int
	Codec    string
	Language string
	Channels int
	Title    string
}

type ChapterInfo struct {
	StartSeconds float64
	EndSeconds   float64
	Title        string
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}

type ffprobeTags struct {
	Language string `json:"language"`
	Title    string `json:"title"`
}

type ffprobeStream struct {
	CodecType string      `json:"codec_type"`
	CodecName string      `json:"codec_name"`
	Width     int         `json:"width"`
	Height    int         `json:"height"`
	Channels  int         `json:"channels"`
	Tags      ffprobeTags `json:"tags"`
}

type ffprobeChapter struct {
	StartTime string      `json:"start_time"`
	EndTime   string      `json:"end_time"`
	Tags      ffprobeTags `json:"tags"`
}

type ffprobeOutput struct {
	Format   ffprobeFormat    `json:"format"`
	Streams  []ffprobeStream  `json:"streams"`
	Chapters []ffprobeChapter `json:"chapters"`
}

// buildHeaderArg formats headers as ffprobe's -headers flag expects: one
// "Name: value" pair per line, CRLF-terminated (including the last one).
// Empty/nil headers returns "" so callers can skip the flag entirely.
func buildHeaderArg(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	var b strings.Builder
	for k, v := range headers {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	return b.String()
}

// Probe runs ffprobe against path and extracts the fields Vorn's playback
// decision (direct play vs. transcode) and player need. headers is nil for
// a local file or a self-authenticating URL (a debrid/NZB CDN link already
// has its token embedded); a WebDAV-backed URL needs Basic Auth passed
// explicitly here since ffprobe has no other way to know it (see
// store.WebDAVProbeHeaders, which every caller of Probe/ProbeWithRetry
// resolves this from).
func Probe(ctx context.Context, path string, headers map[string]string) (*MediaInfo, error) {
	args := []string{
		// "-v error" (not the original "quiet") so a real failure's reason
		// -- a genuinely dead/unreachable link vs. ffprobe timing out mid-
		// analysis vs. a content-level parse error -- actually shows up in
		// the wrapped error below instead of being silently discarded.
		// Callers already only care about the media info on success, so
		// this doesn't change behavior for anyone -- it's purely
		// diagnostic, and was added after a proactive link-health check
		// kept flagging a link as dead despite the same link, and a
		// manual ffprobe against the same URL, both working fine seconds
		// later -- with no error detail at all, that was unfalsifiable.
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-show_chapters",
	}
	if h := buildHeaderArg(headers); h != "" {
		args = append(args, "-headers", h)
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	info, err := parseFFprobeOutput(out)
	if err != nil {
		return nil, err
	}
	info.ContainerNative = isNativeContainer(path)
	return info, nil
}

const (
	probeAttemptTimeout = 8 * time.Second
	probeRetryDelay     = 2 * time.Second
)

// ProbeWithRetry is Probe with one retry after a short delay if the first
// attempt fails. A remote debrid/NZB CDN link occasionally blips (a slow
// response, a transient connection reset) without actually being dead --
// confirmed live: a link that failed this exact probe on three separate
// 30-minute checks in a row (see acquisition.MonitorScheduler.
// refreshDeadLinks) then succeeded immediately on a manual retest and on
// the very next scheduled check, with nothing about the link itself
// having changed. Treating a single failed attempt as "the link is dead"
// was triggering a full re-acquisition (search->score->resolve) for
// content that was actually fine and already cached -- wasted work at
// best, and at worst indistinguishable from a real dead link if indexers
// happen to be having a bad day too. Only reports failure if both
// attempts fail.
//
// Each attempt gets its own bounded sub-timeout rather than splitting the
// caller's context deadline across both, so callers should give ctx
// roughly 2*probeAttemptTimeout+probeRetryDelay of headroom to avoid the
// second attempt getting cut short by their own deadline instead of ever
// running.
func ProbeWithRetry(ctx context.Context, path string, headers map[string]string) (*MediaInfo, error) {
	attempt := func() (*MediaInfo, error) {
		attemptCtx, cancel := context.WithTimeout(ctx, probeAttemptTimeout)
		defer cancel()
		return Probe(attemptCtx, path, headers)
	}

	info, err := attempt()
	if err == nil {
		return info, nil
	}
	select {
	case <-time.After(probeRetryDelay):
	case <-ctx.Done():
		return nil, err
	}
	return attempt()
}

// nativeContainerExts are the file extensions a browser <video>/<audio>
// element can actually demux on its own. Deliberately keyed on extension
// rather than ffprobe's own format_name -- libavformat's matroska demuxer
// reports the same "matroska,webm" format_name for both a real .mkv and a
// browser-native .webm, so format_name alone can't tell them apart; the
// extension the release was actually saved under can.
var nativeContainerExts = map[string]bool{
	".mp4": true, ".m4v": true, ".mov": true, ".webm": true,
	".mp3": true, ".m4a": true, ".aac": true, ".ogg": true, ".flac": true, ".wav": true,
}

func isNativeContainer(path string) bool {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	return nativeContainerExts[strings.ToLower(filepath.Ext(path))]
}

// parseFFprobeOutput is split out from Probe so the JSON-parsing logic can
// be unit tested against a fixture without actually invoking ffprobe.
func parseFFprobeOutput(out []byte) (*MediaInfo, error) {
	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, err
	}

	info := &MediaInfo{}
	if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		info.DurationSeconds = d
	}
	audioIndex := 0
	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			if info.VideoCodec == "" {
				info.VideoCodec = s.CodecName
				info.Width = s.Width
				info.Height = s.Height
			}
		case "audio":
			if info.AudioCodec == "" {
				info.AudioCodec = s.CodecName
			}
			info.AudioTracks = append(info.AudioTracks, AudioTrackInfo{
				Index:    audioIndex,
				Codec:    s.CodecName,
				Language: s.Tags.Language,
				Channels: s.Channels,
				Title:    s.Tags.Title,
			})
			audioIndex++
		}
	}
	for _, c := range parsed.Chapters {
		chapter := ChapterInfo{Title: c.Tags.Title}
		if v, err := strconv.ParseFloat(c.StartTime, 64); err == nil {
			chapter.StartSeconds = v
		}
		if v, err := strconv.ParseFloat(c.EndTime, 64); err == nil {
			chapter.EndSeconds = v
		}
		info.Chapters = append(info.Chapters, chapter)
	}
	return info, nil
}

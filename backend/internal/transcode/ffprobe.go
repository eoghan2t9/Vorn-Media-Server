package transcode

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// Probe runs ffprobe against path and extracts the fields Vorn's playback
// decision (direct play vs. transcode) and player need.
func Probe(ctx context.Context, path string) (*MediaInfo, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-show_chapters",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	info, err := parseFFprobeOutput(out)
	if err != nil {
		return nil, err
	}
	info.ContainerNative = isNativeContainer(path)
	return info, nil
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

package transcode

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
)

type MediaInfo struct {
	DurationSeconds float64
	VideoCodec      string
	AudioCodec      string
	Width           int
	Height          int
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}

type ffprobeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

// Probe runs ffprobe against path and extracts the fields Vorn's playback
// decision (direct play vs. transcode) and player need.
func Probe(ctx context.Context, path string) (*MediaInfo, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, err
	}

	info := &MediaInfo{}
	if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		info.DurationSeconds = d
	}
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
		}
	}
	return info, nil
}

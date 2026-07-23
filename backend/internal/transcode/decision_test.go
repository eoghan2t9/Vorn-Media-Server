package transcode

import "testing"

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		info MediaInfo
		want PlaybackMode
	}{
		{"h264+aac direct plays", MediaInfo{VideoCodec: "h264", AudioCodec: "aac"}, ModeDirect},
		{"vp9+opus direct plays", MediaInfo{VideoCodec: "vp9", AudioCodec: "opus"}, ModeDirect},
		{"hevc needs transcode", MediaInfo{VideoCodec: "hevc", AudioCodec: "aac"}, ModeTranscode},
		{"h264 with dts audio needs transcode", MediaInfo{VideoCodec: "h264", AudioCodec: "dts"}, ModeTranscode},
		{"unknown codecs need transcode", MediaInfo{}, ModeTranscode},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Decide(&c.info); got != c.want {
				t.Errorf("Decide(%+v) = %v, want %v", c.info, got, c.want)
			}
		})
	}
}

package transcode

import "testing"

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		info MediaInfo
		caps ClientCapabilities
		want PlaybackMode
	}{
		{"h264+aac in a native container direct plays", MediaInfo{VideoCodec: "h264", AudioCodec: "aac", ContainerNative: true}, ClientCapabilities{}, ModeDirect},
		{"vp9+opus in a native container direct plays", MediaInfo{VideoCodec: "vp9", AudioCodec: "opus", ContainerNative: true}, ClientCapabilities{}, ModeDirect},
		{"hevc needs transcode with no reported client support", MediaInfo{VideoCodec: "hevc", AudioCodec: "aac", ContainerNative: true}, ClientCapabilities{}, ModeTranscode},
		{"h264 with dts audio only needs a remux (video stream copied as-is)", MediaInfo{VideoCodec: "h264", AudioCodec: "dts", ContainerNative: true}, ClientCapabilities{}, ModeRemux},
		{"hevc with dts audio needs a full transcode (video also incompatible)", MediaInfo{VideoCodec: "hevc", AudioCodec: "dts", ContainerNative: true}, ClientCapabilities{}, ModeTranscode},
		{"unknown codecs need transcode", MediaInfo{}, ClientCapabilities{}, ModeTranscode},
		{"audio-only mp3 direct plays (no video stream at all, container check doesn't apply)", MediaInfo{AudioCodec: "mp3"}, ClientCapabilities{}, ModeDirect},
		{"audio-only with incompatible codec needs transcode, not remux (no video stream to copy)", MediaInfo{AudioCodec: "flac"}, ClientCapabilities{}, ModeTranscode},

		// ClientCapabilities: a client that reports it can actually decode a
		// codec outside the conservative baseline should get direct play
		// instead of paying for a transcode/remux it doesn't need.
		{"hevc direct plays when the client reports HEVC support", MediaInfo{VideoCodec: "hevc", AudioCodec: "aac", ContainerNative: true}, ClientCapabilities{VideoCodecs: []string{"hevc"}}, ModeDirect},
		{"h264+ac3 direct plays when the client reports AC3 support", MediaInfo{VideoCodec: "h264", AudioCodec: "ac3", ContainerNative: true}, ClientCapabilities{AudioCodecs: []string{"ac3"}}, ModeDirect},
		{"hevc+dts direct plays when the client reports both", MediaInfo{VideoCodec: "hevc", AudioCodec: "dts", ContainerNative: true}, ClientCapabilities{VideoCodecs: []string{"hevc"}, AudioCodecs: []string{"dts"}}, ModeDirect},
		{"reporting an unrelated codec doesn't help", MediaInfo{VideoCodec: "hevc", AudioCodec: "aac", ContainerNative: true}, ClientCapabilities{VideoCodecs: []string{"av1"}}, ModeTranscode},

		// ContainerNative: no mainstream browser can demux Matroska at all,
		// regardless of how compatible the codecs inside it are -- by far
		// the most common scene/usenet release container, so this is not
		// an edge case.
		{"h264+aac in a non-native container (.mkv) needs a remux, not direct play", MediaInfo{VideoCodec: "h264", AudioCodec: "aac", ContainerNative: false}, ClientCapabilities{}, ModeRemux},
		{"h264+dts in a non-native container still just needs a remux, same as container-only", MediaInfo{VideoCodec: "h264", AudioCodec: "dts", ContainerNative: false}, ClientCapabilities{}, ModeRemux},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Decide(&c.info, c.caps); got != c.want {
				t.Errorf("Decide(%+v, %+v) = %v, want %v", c.info, c.caps, got, c.want)
			}
		})
	}
}

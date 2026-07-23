package transcode

// PlaybackMode is what the player should do with a given file.
type PlaybackMode string

const (
	ModeDirect    PlaybackMode = "direct"
	ModeTranscode PlaybackMode = "transcode"
)

// webCompatibleVideoCodecs / webCompatibleAudioCodecs are broadly playable
// by browsers without server-side transcoding.
var webCompatibleVideoCodecs = map[string]bool{"h264": true, "vp9": true, "vp8": true, "av1": true}
var webCompatibleAudioCodecs = map[string]bool{"aac": true, "opus": true, "mp3": true}

// Decide reports whether info can be sent to the browser as-is (direct
// play) or needs transcoding to HLS first.
func Decide(info *MediaInfo) PlaybackMode {
	if webCompatibleVideoCodecs[info.VideoCodec] && webCompatibleAudioCodecs[info.AudioCodec] {
		return ModeDirect
	}
	return ModeTranscode
}

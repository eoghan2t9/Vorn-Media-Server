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
	// An audio-only file (music track, audiobook chapter) has no video stream
	// at all -- info.VideoCodec is "", which must not be treated the same as
	// an actually-incompatible video codec, or every audio file would be
	// needlessly forced through transcoding even when the audio codec alone
	// is already browser-playable.
	videoOK := info.VideoCodec == "" || webCompatibleVideoCodecs[info.VideoCodec]
	if videoOK && webCompatibleAudioCodecs[info.AudioCodec] {
		return ModeDirect
	}
	return ModeTranscode
}

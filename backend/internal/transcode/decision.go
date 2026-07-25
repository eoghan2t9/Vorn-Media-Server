package transcode

// PlaybackMode is what the player should do with a given file.
type PlaybackMode string

const (
	ModeDirect PlaybackMode = "direct"
	// ModeRemux means the video stream is already web-compatible and can be
	// copied as-is -- only the audio codec needs converting, so the
	// resulting HLS session skips video re-encoding entirely (see
	// Manager.StartSession's copyVideo). This is the common case of e.g.
	// H.264 video paired with DTS/AC3/TrueHD audio (frequent on
	// scene/remux releases): stream-copying video is nearly free (no
	// CPU-bound encode at all, just re-muxing), turning what would
	// otherwise be a full re-encode into a near-instant HLS start.
	ModeRemux     PlaybackMode = "remux"
	ModeTranscode PlaybackMode = "transcode"
)

// webCompatibleVideoCodecs / webCompatibleAudioCodecs are the universally
// safe baseline -- broadly playable by any browser without server-side
// transcoding, regardless of the specific client. See ClientCapabilities
// for codecs only some clients can additionally handle.
var webCompatibleVideoCodecs = map[string]bool{"h264": true, "vp9": true, "vp8": true, "av1": true}
var webCompatibleAudioCodecs = map[string]bool{"aac": true, "opus": true, "mp3": true}

// ClientCapabilities describes codecs the specific requesting player/
// browser can decode natively, beyond the universally-safe baseline above
// -- e.g. a modern phone's hardware HEVC decoder, or Safari's native
// AC3/EAC3 support. Reported by the frontend (via video.canPlayType())
// before requesting playback, so Decide can skip transcoding entirely on
// devices that don't need it instead of applying one conservative global
// codec list to every client regardless of what it can actually play.
// The zero value reports no additional capabilities, preserving the
// original baseline-only behavior for any caller that doesn't have (or
// send) this information.
type ClientCapabilities struct {
	VideoCodecs []string
	AudioCodecs []string
}

func (c ClientCapabilities) supportsVideo(codec string) bool {
	for _, v := range c.VideoCodecs {
		if v == codec {
			return true
		}
	}
	return false
}

func (c ClientCapabilities) supportsAudio(codec string) bool {
	for _, a := range c.AudioCodecs {
		if a == codec {
			return true
		}
	}
	return false
}

// Decide reports whether info can be sent to caps's client as-is (direct
// play), needs its video stream copied as-is with only audio converted
// (remux -- see ModeRemux), or needs a full re-encode (transcode).
func Decide(info *MediaInfo, caps ClientCapabilities) PlaybackMode {
	// An audio-only file (music track, audiobook chapter) has no video stream
	// at all -- info.VideoCodec is "", which must not be treated the same as
	// an actually-incompatible video codec, or every audio file would be
	// needlessly forced through transcoding even when the audio codec alone
	// is already browser-playable. It also must not be eligible for
	// ModeRemux below (there is no video stream to "copy"), which is why
	// that branch additionally requires hasVideo.
	hasVideo := info.VideoCodec != ""
	videoCompatible := !hasVideo || webCompatibleVideoCodecs[info.VideoCodec] || caps.supportsVideo(info.VideoCodec)
	audioCompatible := webCompatibleAudioCodecs[info.AudioCodec] || caps.supportsAudio(info.AudioCodec)

	switch {
	case videoCompatible && audioCompatible:
		return ModeDirect
	case hasVideo && videoCompatible:
		return ModeRemux
	default:
		return ModeTranscode
	}
}

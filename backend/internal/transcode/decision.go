package transcode

// PlaybackMode is what the player should do with a given file.
type PlaybackMode string

const (
	ModeDirect PlaybackMode = "direct"
	// ModeRemux means the video stream is already web-compatible and can be
	// copied as-is -- only the container and/or audio codec need fixing, so
	// ffmpeg just repackages into a plain progressive MP4 (see
	// Manager.StartSession's copyVideo) instead of transcoding video at all.
	// This covers both the classic "H.264 video, DTS/AC3/TrueHD audio"
	// scene-release case AND the even more common "everything's actually
	// compatible, it's just wrapped in an .mkv" case (mainstream browsers
	// can't demux Matroska at all, regardless of the codecs inside it --
	// see MediaInfo.ContainerNative). Stream-copying video is nearly free
	// (no CPU-bound encode, just re-muxing), so this is a near-instant
	// start either way.
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
// play), needs repackaging into a plain MP4 with its video stream copied
// as-is (remux -- see ModeRemux), or needs a full re-encode (transcode).
func Decide(info *MediaInfo, caps ClientCapabilities) PlaybackMode {
	// An audio-only file (music track, audiobook chapter) has no video stream
	// at all -- info.VideoCodec is "", which must not be treated the same as
	// an actually-incompatible video codec, or every audio file would be
	// needlessly forced through transcoding even when the audio codec alone
	// is already browser-playable. It also must not be eligible for
	// ModeRemux below (there is no video stream to "copy"), which is why
	// that branch additionally requires hasVideo. The container check is
	// skipped for audio-only too -- direct play failures there are a codec
	// problem, not a "browser can't demux this container" one, the way
	// video-in-.mkv specifically is.
	hasVideo := info.VideoCodec != ""
	videoCompatible := !hasVideo || webCompatibleVideoCodecs[info.VideoCodec] || caps.supportsVideo(info.VideoCodec)
	audioCompatible := webCompatibleAudioCodecs[info.AudioCodec] || caps.supportsAudio(info.AudioCodec)
	containerCompatible := !hasVideo || info.ContainerNative

	switch {
	case videoCompatible && audioCompatible && containerCompatible:
		return ModeDirect
	case hasVideo && videoCompatible:
		return ModeRemux
	default:
		return ModeTranscode
	}
}

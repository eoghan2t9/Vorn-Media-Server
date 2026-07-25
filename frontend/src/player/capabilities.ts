// detectClientCapabilities reports codecs this specific browser/device can
// decode natively, beyond the conservative baseline the backend always
// treats as web-compatible (h264/vp9/vp8/av1 video, aac/opus/mp3 audio).
// Sent with playItem() so transcode.Decide (backend) can grant direct play
// on devices that don't need a transcode/remux -- e.g. a modern phone with
// hardware HEVC decode, or Safari's native AC3/EAC3 support. Codec names
// match ffprobe's codec_name values (transcode.MediaInfo), since that's what
// the backend compares them against.
export interface ClientCapabilities {
  videoCodecs: string[]
  audioCodecs: string[]
}

let cached: ClientCapabilities | null = null

export function detectClientCapabilities(): ClientCapabilities {
  if (cached) return cached

  const probe = document.createElement('video')
  const canPlay = (mime: string) => probe.canPlayType(mime) !== ''

  const videoCodecs: string[] = []
  if (canPlay('video/mp4; codecs="hvc1.1.6.L93.B0"') || canPlay('video/mp4; codecs="hev1.1.6.L93.B0"')) {
    videoCodecs.push('hevc')
  }

  const audioCodecs: string[] = []
  if (canPlay('audio/mp4; codecs="ac-3"')) audioCodecs.push('ac3')
  if (canPlay('audio/mp4; codecs="ec-3"')) audioCodecs.push('eac3')

  cached = { videoCodecs, audioCodecs }
  return cached
}

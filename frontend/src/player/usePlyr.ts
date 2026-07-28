import Plyr from 'plyr'
import { useEffect, type RefObject } from 'react'

// Overrides the native video.duration getter on the <video> element with the
// backend-probed duration (from ffprobe) stored in video.dataset.duration.
//
// The browser's native duration for a streamed/debrid-backed video is
// unreliable — it starts as Infinity, then jumps through partial values
// (12s → 20s → 30s …) as more data arrives. Our ffprobe probe is always
// authoritative. By overriding at the element property level, both Plyr
// (which reads video.duration natively) and the browser's own seek-bar/
// ended-event machinery see the correct total runtime immediately, with no
// DOM-patching race conditions against Plyr's control-bar render cycle.
function patchVideoDuration(video: HTMLVideoElement) {
  const known = video.dataset.duration
  if (!known) return
  const dur = parseFloat(known)
  if (!dur || dur <= 0) return

  try {
    Object.defineProperty(video, 'duration', {
      get() { return dur },
      configurable: true,
      enumerable: true,
    })
  } catch {
    // Object.defineProperty on native getters may not work in all browsers.
    // If it fails, the video plays without the patched duration — the
    // browser's unreliable duration will be used instead, same as before.
  }
}

// usePlyr wraps Plyr (a mature, battle-tested player UI with years of
// production hardening around exactly the mobile/autoplay/direct-play
// quirks a hand-rolled control bar kept tripping over) around an
// already-existing <video> element, once, for the lifetime of the
// component. Plyr "enhances" the native element in place -- it does not
// replace or take ownership of it, so WatchPage's own hls.js attach/
// recovery/progress-reporting logic (which reads/writes video.currentTime,
// calls video.play(), etc. directly) keeps working completely unchanged;
// Plyr only adds the UI chrome and reacts to the same native media events.
export function usePlyr(videoRef: RefObject<HTMLVideoElement | null>) {
  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    const player = new Plyr(video, {
      controls: ['play-large', 'play', 'progress', 'current-time', 'duration', 'mute', 'volume', 'settings', 'pip', 'airplay', 'fullscreen'],
      settings: ['speed'],
      speed: { selected: 1, options: [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] },
      seekTime: 10,
      keyboard: { focused: true, global: false },
    })

    // Override native video.duration with the backend-probed value (see
    // patchVideoDuration above) so Plyr and the browser both see the
    // correct total runtime immediately.
    patchVideoDuration(video)

    return () => player.destroy()
  }, [videoRef])
}

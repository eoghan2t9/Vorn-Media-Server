import Plyr from 'plyr'
import { useEffect, type RefObject } from 'react'

// Formats seconds into h:mm:ss for display.
function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  return `${m}:${String(s).padStart(2, '0')}`
}

// Patches the duration display in the Plyr control bar to always show the
// backend-probed duration (obtained via ffprobe) rather than the browser's
// native video.duration. The native duration for a streamed/debrid-backed
// video is unreliable — it starts as Infinity, then jumps through partial
// values (12s → 20s → 30s …) as more data arrives. Our ffprobe probe is
// always authoritative.
function patchDurationDisplay(video: HTMLVideoElement) {
  const knownDuration = video.dataset.duration
  if (!knownDuration) return
  const dur = parseFloat(knownDuration)
  if (!dur || dur <= 0) return

  const overridePlyrDuration = () => {
    // Plyr renders the duration into a .plyr__time[data-plyr="duration"] element
    // on every timeupdate/loadedmetadata — re-apply our value each time.
    const durEl = document.querySelector('.plyr__time[data-plyr="duration"]')
    if (durEl && durEl.textContent !== formatDuration(dur)) {
      durEl.textContent = formatDuration(dur)
    }
  }

  video.addEventListener('timeupdate', overridePlyrDuration)
  video.addEventListener('loadedmetadata', overridePlyrDuration)
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

    // Patch the duration display using backend-probed duration if the
    // browser cannot determine it from the stream alone.
    patchDurationDisplay(video)

    return () => player.destroy()
  }, [videoRef])
}

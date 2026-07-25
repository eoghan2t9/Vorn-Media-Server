import Plyr from 'plyr'
import { useEffect, type RefObject } from 'react'

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

    return () => player.destroy()
  }, [videoRef])
}

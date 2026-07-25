import { useCallback, useEffect, useRef, useState, type PointerEvent, type RefObject } from 'react'
import type { AudioTrack, Chapter } from '../api/client'
import { subtitlesUrl } from '../api/client'
import { useKeyboardShortcuts } from './useKeyboardShortcuts'
import './PlayerShell.css'

const SUBTITLE_LANGUAGES = [
  { code: 'off', label: 'Off' },
  { code: 'en', label: 'English' },
  { code: 'es', label: 'Spanish' },
  { code: 'fr', label: 'French' },
  { code: 'de', label: 'German' },
  { code: 'it', label: 'Italian' },
  { code: 'pt', label: 'Portuguese' },
  { code: 'ru', label: 'Russian' },
  { code: 'ja', label: 'Japanese' },
]

const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]
const CONTROLS_HIDE_DELAY_MS = 3000
const DOUBLE_TAP_WINDOW_MS = 300
const INTRO_TITLE_PATTERN = /^(intro|opening|recap)/i

function formatTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  const mm = h > 0 ? String(m).padStart(2, '0') : String(m)
  const ss = String(s).padStart(2, '0')
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`
}

interface PlayerShellProps {
  videoRef: RefObject<HTMLVideoElement | null>
  itemId: string
  hidden: boolean
  subtitleLanguage: string
  onSubtitleChange: (language: string) => void
  audioTracks: AudioTrack[]
  audioTrackIndex: number | undefined
  onAudioTrackChange: (index: number) => void
  chapters: Chapter[]
  onTimeUpdate?: () => void
  onEnded?: () => void
}

// PlayerShell replaces the browser's native <video controls> chrome with a
// themed control bar plus the extra playback features (audio/subtitle
// picker, skip-intro, keyboard shortcuts, touch gestures). It does NOT own
// the streaming session itself -- WatchPage still drives hls.js/attach/
// recovery/progress-reporting against the same videoRef, this component
// only reflects and controls native <video> element state (play/pause,
// currentTime, volume, playbackRate).
export function PlayerShell({
  videoRef,
  itemId,
  hidden,
  subtitleLanguage,
  onSubtitleChange,
  audioTracks,
  audioTrackIndex,
  onAudioTrackChange,
  chapters,
  onTimeUpdate,
  onEnded,
}: PlayerShellProps) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const lastTapRef = useRef<{ side: 'left' | 'right'; time: number } | null>(null)

  const [isPlaying, setIsPlaying] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [bufferedEnd, setBufferedEnd] = useState(0)
  const [volume, setVolume] = useState(1)
  const [muted, setMuted] = useState(false)
  const [playbackRate, setPlaybackRate] = useState(1)
  const [controlsVisible, setControlsVisible] = useState(true)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [seekFlash, setSeekFlash] = useState<'back' | 'forward' | null>(null)

  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    const onPlay = () => setIsPlaying(true)
    const onPause = () => setIsPlaying(false)
    const onTime = () => {
      setCurrentTime(video.currentTime)
      onTimeUpdate?.()
    }
    const onDuration = () => setDuration(video.duration || 0)
    const onVolume = () => {
      setVolume(video.volume)
      setMuted(video.muted)
    }
    const onRate = () => setPlaybackRate(video.playbackRate)
    const onProgress = () => {
      const { buffered } = video
      setBufferedEnd(buffered.length > 0 ? buffered.end(buffered.length - 1) : 0)
    }

    video.addEventListener('play', onPlay)
    video.addEventListener('pause', onPause)
    video.addEventListener('timeupdate', onTime)
    video.addEventListener('durationchange', onDuration)
    video.addEventListener('volumechange', onVolume)
    video.addEventListener('ratechange', onRate)
    video.addEventListener('progress', onProgress)
    return () => {
      video.removeEventListener('play', onPlay)
      video.removeEventListener('pause', onPause)
      video.removeEventListener('timeupdate', onTime)
      video.removeEventListener('durationchange', onDuration)
      video.removeEventListener('volumechange', onVolume)
      video.removeEventListener('ratechange', onRate)
      video.removeEventListener('progress', onProgress)
    }
  }, [videoRef, onTimeUpdate])

  const showControls = useCallback(() => {
    setControlsVisible(true)
    if (hideTimerRef.current) clearTimeout(hideTimerRef.current)
    if (isPlaying) {
      hideTimerRef.current = setTimeout(() => setControlsVisible(false), CONTROLS_HIDE_DELAY_MS)
    }
  }, [isPlaying])

  useEffect(() => {
    showControls()
    return () => {
      if (hideTimerRef.current) clearTimeout(hideTimerRef.current)
    }
  }, [showControls])

  function togglePlay() {
    const video = videoRef.current
    if (!video) return
    if (video.paused) video.play().catch(() => {})
    else video.pause()
  }

  function seek(deltaSeconds: number) {
    const video = videoRef.current
    if (!video || !Number.isFinite(video.duration)) return
    video.currentTime = Math.min(Math.max(video.currentTime + deltaSeconds, 0), video.duration)
  }

  function seekToFraction(fraction: number) {
    const video = videoRef.current
    if (!video || !Number.isFinite(video.duration)) return
    video.currentTime = video.duration * fraction
  }

  function seekToClientX(clientX: number, track: HTMLElement) {
    const video = videoRef.current
    if (!video || !Number.isFinite(video.duration)) return
    const rect = track.getBoundingClientRect()
    const fraction = Math.min(Math.max((clientX - rect.left) / rect.width, 0), 1)
    video.currentTime = video.duration * fraction
  }

  function changeVolume(delta: number) {
    const video = videoRef.current
    if (!video) return
    video.muted = false
    video.volume = Math.min(Math.max(video.volume + delta, 0), 1)
  }

  function toggleMute() {
    const video = videoRef.current
    if (!video) return
    video.muted = !video.muted
  }

  function changeSpeed(delta: number) {
    const video = videoRef.current
    if (!video) return
    const idx = SPEEDS.reduce((closest, s, i) => (Math.abs(s - video.playbackRate) < Math.abs(SPEEDS[closest] - video.playbackRate) ? i : closest), 0)
    const nextIdx = Math.min(Math.max(idx + (delta > 0 ? 1 : -1), 0), SPEEDS.length - 1)
    video.playbackRate = SPEEDS[nextIdx]
  }

  function toggleFullscreen() {
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {})
    } else {
      wrapRef.current?.requestFullscreen().catch(() => {})
    }
  }

  function togglePiP() {
    const video = videoRef.current
    if (!video) return
    if (document.pictureInPictureElement) {
      document.exitPictureInPicture().catch(() => {})
    } else {
      video.requestPictureInPicture().catch(() => {})
    }
  }

  useKeyboardShortcuts(
    {
      onTogglePlay: togglePlay,
      onSeek: seek,
      onSeekToFraction: seekToFraction,
      onVolumeChange: changeVolume,
      onToggleMute: toggleMute,
      onToggleFullscreen: toggleFullscreen,
      onSpeedChange: changeSpeed,
    },
    !hidden,
  )

  const activeChapter = chapters.find((c) => currentTime >= c.startSeconds && currentTime < c.endSeconds)
  const showSkipIntro = !!activeChapter && INTRO_TITLE_PATTERN.test(activeChapter.title ?? '')

  function handleVideoTap(e: PointerEvent<HTMLDivElement>) {
    if (e.pointerType !== 'touch') {
      showControls()
      return
    }
    const rect = e.currentTarget.getBoundingClientRect()
    const side: 'left' | 'right' = e.clientX - rect.left < rect.width / 2 ? 'left' : 'right'
    const now = Date.now()
    const last = lastTapRef.current
    if (last && last.side === side && now - last.time < DOUBLE_TAP_WINDOW_MS) {
      seek(side === 'left' ? -10 : 10)
      setSeekFlash(side === 'left' ? 'back' : 'forward')
      setTimeout(() => setSeekFlash(null), 500)
      lastTapRef.current = null
    } else {
      lastTapRef.current = { side, time: now }
      setControlsVisible((v) => !v)
    }
  }

  const progressPct = duration > 0 ? (currentTime / duration) * 100 : 0
  const bufferedPct = duration > 0 ? (bufferedEnd / duration) * 100 : 0

  return (
    <div
      ref={wrapRef}
      className={`vorn-player ${controlsVisible ? '' : 'vorn-player-controls-hidden'}`}
      onPointerMove={() => showControls()}
    >
      <video ref={videoRef} className="vorn-video" onEnded={onEnded}>
        {itemId && subtitleLanguage !== 'off' && (
          <track key={subtitleLanguage} kind="subtitles" src={subtitlesUrl(itemId, subtitleLanguage)} srcLang={subtitleLanguage} default />
        )}
      </video>

      <div className="vorn-player-tap-zone" onPointerUp={handleVideoTap}>
        {seekFlash && <span className={`vorn-player-seek-flash vorn-player-seek-flash-${seekFlash}`}>{seekFlash === 'back' ? '-10s' : '+10s'}</span>}
      </div>

      {!hidden && (
        <div className="vorn-player-controls" onPointerDown={(e) => e.stopPropagation()}>
          <div
            className="vorn-player-scrubber"
            onPointerDown={(e) => {
              seekToClientX(e.clientX, e.currentTarget)
              showControls()
            }}
          >
            <div className="vorn-player-scrubber-buffered" style={{ width: `${bufferedPct}%` }} />
            <div className="vorn-player-scrubber-progress" style={{ width: `${progressPct}%` }} />
            {chapters.map((c, i) => (
              <div key={i} className="vorn-player-chapter-tick" style={{ left: `${duration > 0 ? (c.startSeconds / duration) * 100 : 0}%` }} />
            ))}
          </div>

          <div className="vorn-player-row">
            <button type="button" onClick={togglePlay} aria-label={isPlaying ? 'Pause' : 'Play'}>
              {isPlaying ? '⏸' : '▶'}
            </button>
            <button type="button" onClick={() => seek(-10)} aria-label="Back 10 seconds">
              ⏪
            </button>
            <button type="button" onClick={() => seek(10)} aria-label="Forward 10 seconds">
              ⏩
            </button>

            <span className="vorn-player-time">
              {formatTime(currentTime)} / {formatTime(duration)}
            </span>

            {showSkipIntro && activeChapter && (
              <button type="button" className="vorn-player-skip-intro" onClick={() => seek(activeChapter.endSeconds - currentTime)}>
                Skip Intro
              </button>
            )}

            <span className="vorn-player-spacer" />

            <button type="button" onClick={toggleMute} aria-label={muted || volume === 0 ? 'Unmute' : 'Mute'}>
              {muted || volume === 0 ? '🔇' : '🔊'}
            </button>
            <input
              className="vorn-player-volume"
              type="range"
              min={0}
              max={1}
              step={0.05}
              value={muted ? 0 : volume}
              onChange={(e) => {
                const video = videoRef.current
                if (!video) return
                video.muted = false
                video.volume = Number(e.target.value)
              }}
            />

            <div className="vorn-player-settings">
              <button type="button" onClick={() => setSettingsOpen((v) => !v)} aria-label="Settings">
                ⚙
              </button>
              {settingsOpen && (
                <div className="vorn-player-settings-menu">
                  <label>
                    Speed
                    <select value={playbackRate} onChange={(e) => (videoRef.current!.playbackRate = Number(e.target.value))}>
                      {SPEEDS.map((s) => (
                        <option key={s} value={s}>
                          {s}×
                        </option>
                      ))}
                    </select>
                  </label>
                  {audioTracks.length > 1 && (
                    <label>
                      Audio
                      <select value={audioTrackIndex ?? audioTracks[0]?.index ?? 0} onChange={(e) => onAudioTrackChange(Number(e.target.value))}>
                        {audioTracks.map((t) => (
                          <option key={t.index} value={t.index}>
                            {t.title || t.language || t.codec} {t.channels ? `(${t.channels}ch)` : ''}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                  <label>
                    Subtitles
                    <select value={subtitleLanguage} onChange={(e) => onSubtitleChange(e.target.value)}>
                      {SUBTITLE_LANGUAGES.map((l) => (
                        <option key={l.code} value={l.code}>
                          {l.label}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
              )}
            </div>

            {document.pictureInPictureEnabled && (
              <button type="button" onClick={togglePiP} aria-label="Picture in picture">
                ⧉
              </button>
            )}
            <button type="button" onClick={toggleFullscreen} aria-label="Fullscreen">
              ⛶
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

import { useCallback, useEffect, useRef, useState, type PointerEvent, type RefObject } from 'react'
import type { AudioTrack, Chapter } from '../api/client'
import { subtitlesUrl } from '../api/client'
import {
  CheckIcon,
  ChevronRightIcon,
  FullscreenExitIcon,
  FullscreenIcon,
  GearIcon,
  PauseIcon,
  PipIcon,
  PlayIcon,
  SeekBackIcon,
  SeekForwardIcon,
  VolumeIcon,
  VolumeMuteIcon,
} from '../components/icons'
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

type SettingsPanel = 'main' | 'speed' | 'audio' | 'subtitles'

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
//
// The settings menu is a plain absolutely-positioned panel (not the shared
// portaled <Select>) deliberately -- fullscreen here is the real Fullscreen
// API (wrapRef.requestFullscreen()), and only the fullscreened element's own
// descendants render while active. A portal to document.body would be
// invisible the moment a viewer actually goes fullscreen, which is the most
// common way to watch on a phone.
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
  const [settingsPanel, setSettingsPanel] = useState<SettingsPanel | null>(null)
  const [seekFlash, setSeekFlash] = useState<'back' | 'forward' | null>(null)
  const [centerBounce, setCenterBounce] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)

  useEffect(() => {
    const onChange = () => setIsFullscreen(!!document.fullscreenElement)
    document.addEventListener('fullscreenchange', onChange)
    return () => document.removeEventListener('fullscreenchange', onChange)
  }, [])

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
    setCenterBounce(true)
    setTimeout(() => setCenterBounce(false), 400)
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
  const activeAudioTrack = audioTracks.find((t) => t.index === (audioTrackIndex ?? audioTracks[0]?.index))
  const activeSubtitleLabel = SUBTITLE_LANGUAGES.find((l) => l.code === subtitleLanguage)?.label ?? 'Off'

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

  function closeSettings() {
    setSettingsPanel(null)
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
        {seekFlash && (
          <span className={`vorn-player-seek-flash vorn-player-seek-flash-${seekFlash}`}>
            {seekFlash === 'back' ? <SeekBackIcon width={28} height={28} /> : <SeekForwardIcon width={28} height={28} />}
          </span>
        )}
      </div>

      {!hidden && (
        <button
          type="button"
          className={`vorn-player-center-btn ${centerBounce ? 'vorn-player-center-btn-bounce' : ''}`}
          onClick={togglePlay}
          aria-label={isPlaying ? 'Pause' : 'Play'}
          tabIndex={-1}
        >
          {isPlaying ? <PauseIcon width={30} height={30} /> : <PlayIcon width={30} height={30} />}
        </button>
      )}

      {!hidden && (
        <div className="vorn-player-controls" onPointerDown={(e) => e.stopPropagation()}>
          <div
            className="vorn-player-scrubber"
            onPointerDown={(e) => {
              seekToClientX(e.clientX, e.currentTarget)
              showControls()
            }}
          >
            <div className="vorn-player-scrubber-track">
              <div className="vorn-player-scrubber-buffered" style={{ width: `${bufferedPct}%` }} />
              <div className="vorn-player-scrubber-progress" style={{ width: `${progressPct}%` }} />
              {chapters.map((c, i) => (
                <div key={i} className="vorn-player-chapter-tick" style={{ left: `${duration > 0 ? (c.startSeconds / duration) * 100 : 0}%` }} />
              ))}
              <div className="vorn-player-scrubber-thumb" style={{ left: `${progressPct}%` }} />
            </div>
          </div>

          <div className="vorn-player-row">
            <button type="button" className="vorn-player-btn" onClick={togglePlay} aria-label={isPlaying ? 'Pause' : 'Play'}>
              {isPlaying ? <PauseIcon /> : <PlayIcon />}
            </button>
            <button type="button" className="vorn-player-btn" onClick={() => seek(-10)} aria-label="Back 10 seconds">
              <SeekBackIcon />
            </button>
            <button type="button" className="vorn-player-btn" onClick={() => seek(10)} aria-label="Forward 10 seconds">
              <SeekForwardIcon />
            </button>

            <div className="vorn-player-volume-group">
              <button type="button" className="vorn-player-btn" onClick={toggleMute} aria-label={muted || volume === 0 ? 'Unmute' : 'Mute'}>
                {muted || volume === 0 ? <VolumeMuteIcon /> : <VolumeIcon />}
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
                aria-label="Volume"
              />
            </div>

            <span className="vorn-player-time">
              {formatTime(currentTime)} <span className="vorn-player-time-sep">/</span> {formatTime(duration)}
            </span>

            {showSkipIntro && activeChapter && (
              <button type="button" className="vorn-player-skip-intro" onClick={() => seek(activeChapter.endSeconds - currentTime)}>
                Skip Intro
              </button>
            )}

            <span className="vorn-player-spacer" />

            <div className="vorn-player-settings">
              <button
                type="button"
                className="vorn-player-btn"
                onClick={() => setSettingsPanel((v) => (v ? null : 'main'))}
                aria-label="Settings"
              >
                <GearIcon />
              </button>
              {settingsPanel && (
                <div className="vorn-player-settings-menu">
                  {settingsPanel === 'main' && (
                    <>
                      <button type="button" className="vorn-player-menu-row" onClick={() => setSettingsPanel('speed')}>
                        <span>Speed</span>
                        <span className="vorn-player-menu-value">
                          {playbackRate}× <ChevronRightIcon width={14} height={14} />
                        </span>
                      </button>
                      {audioTracks.length > 1 && (
                        <button type="button" className="vorn-player-menu-row" onClick={() => setSettingsPanel('audio')}>
                          <span>Audio</span>
                          <span className="vorn-player-menu-value">
                            {activeAudioTrack?.title || activeAudioTrack?.language || activeAudioTrack?.codec || 'Default'}{' '}
                            <ChevronRightIcon width={14} height={14} />
                          </span>
                        </button>
                      )}
                      <button type="button" className="vorn-player-menu-row" onClick={() => setSettingsPanel('subtitles')}>
                        <span>Subtitles</span>
                        <span className="vorn-player-menu-value">
                          {activeSubtitleLabel} <ChevronRightIcon width={14} height={14} />
                        </span>
                      </button>
                    </>
                  )}

                  {settingsPanel === 'speed' && (
                    <>
                      <button type="button" className="vorn-player-menu-back" onClick={() => setSettingsPanel('main')}>
                        Speed
                      </button>
                      {SPEEDS.map((s) => (
                        <button
                          key={s}
                          type="button"
                          className="vorn-player-menu-option"
                          onClick={() => {
                            if (videoRef.current) videoRef.current.playbackRate = s
                            closeSettings()
                          }}
                        >
                          <span>{s}×</span>
                          {playbackRate === s && <CheckIcon width={15} height={15} />}
                        </button>
                      ))}
                    </>
                  )}

                  {settingsPanel === 'audio' && (
                    <>
                      <button type="button" className="vorn-player-menu-back" onClick={() => setSettingsPanel('main')}>
                        Audio
                      </button>
                      {audioTracks.map((t) => (
                        <button
                          key={t.index}
                          type="button"
                          className="vorn-player-menu-option"
                          onClick={() => {
                            onAudioTrackChange(t.index)
                            closeSettings()
                          }}
                        >
                          <span>
                            {t.title || t.language || t.codec} {t.channels ? `(${t.channels}ch)` : ''}
                          </span>
                          {(audioTrackIndex ?? audioTracks[0]?.index) === t.index && <CheckIcon width={15} height={15} />}
                        </button>
                      ))}
                    </>
                  )}

                  {settingsPanel === 'subtitles' && (
                    <>
                      <button type="button" className="vorn-player-menu-back" onClick={() => setSettingsPanel('main')}>
                        Subtitles
                      </button>
                      {SUBTITLE_LANGUAGES.map((l) => (
                        <button
                          key={l.code}
                          type="button"
                          className="vorn-player-menu-option"
                          onClick={() => {
                            onSubtitleChange(l.code)
                            closeSettings()
                          }}
                        >
                          <span>{l.label}</span>
                          {subtitleLanguage === l.code && <CheckIcon width={15} height={15} />}
                        </button>
                      ))}
                    </>
                  )}
                </div>
              )}
            </div>

            {document.pictureInPictureEnabled && (
              <button type="button" className="vorn-player-btn" onClick={togglePiP} aria-label="Picture in picture">
                <PipIcon />
              </button>
            )}
            <button type="button" className="vorn-player-btn" onClick={toggleFullscreen} aria-label="Fullscreen">
              {isFullscreen ? <FullscreenExitIcon /> : <FullscreenIcon />}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

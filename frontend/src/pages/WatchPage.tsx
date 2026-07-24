import Hls from 'hls.js'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  API_BASE,
  ApiError,
  getItem,
  getProgress,
  playItem,
  stopStreamSession,
  subtitlesUrl,
  updateProgress,
  type MediaItem,
  type PlayResponse,
} from '../api/client'
import { findNextEpisode } from '../player/nextEpisode'
import './WatchPage.css'

const PROGRESS_REPORT_INTERVAL_MS = 5000
const NEAR_END_THRESHOLD_SECONDS = 30
const ACQUISITION_POLL_INTERVAL_MS = 2000
// Bounds how many times a single playback session auto-recovers from a
// mid-stream failure (e.g. a debrid link expiring while playing) before
// giving up and asking the viewer to retry manually -- without a cap, a
// provider that's genuinely down would retry-loop indefinitely instead of
// surfacing that something's wrong.
const MAX_MIDSTREAM_RETRIES = 2

type AcquiringState = { status: 'searching' | 'acquiring' | 'error'; message?: string }

const ACQUIRING_COPY: Record<'searching' | 'acquiring', string> = {
  searching: 'Searching for a source…',
  acquiring: 'Preparing your stream…',
}

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

export function WatchPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const hlsRef = useRef<Hls | null>(null)
  const sessionIdRef = useRef<string | null>(null)

  const [item, setItem] = useState<MediaItem | null>(null)
  const [nextEpisode, setNextEpisode] = useState<MediaItem | null>(null)
  const [showUpNext, setShowUpNext] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [subtitleLanguage, setSubtitleLanguage] = useState('off')
  const [acquiring, setAcquiring] = useState<AcquiringState | null>(null)
  const [retryKey, setRetryKey] = useState(0)

  useEffect(() => {
    if (!id) return
    const video = videoRef.current
    if (!video) return

    let cancelled = false
    let progressTimer: ReturnType<typeof setInterval> | undefined
    let acquisitionTimer: ReturnType<typeof setInterval> | undefined
    // Recovery budget for THIS mount/retry cycle -- reset to 0 on every
    // successful handlePlayResponse (see below) rather than only once per
    // page load, so a couple of early blips don't burn through the whole
    // budget and leave a two-hour movie with no recovery left for later.
    let midStreamRetries = 0
    let recovering = false

    async function attach(video: HTMLVideoElement, play: PlayResponse) {
      if (play.sessionId) sessionIdRef.current = play.sessionId
      const url = play.mode === 'direct' ? `${API_BASE}${play.directUrl}` : `${API_BASE}${play.playlistUrl}`

      if (play.mode === 'transcode' && Hls.isSupported()) {
        const hls = new Hls()
        hlsRef.current = hls
        // Only fatal errors -- hls.js already retries recoverable ones
        // (a dropped segment, a transient stall) internally, so reacting to
        // every error here would fight its own recovery instead of only
        // stepping in once it's actually given up.
        hls.on(Hls.Events.ERROR, (_event, data) => {
          if (data.fatal) recoverFromPlaybackError()
        })
        hls.loadSource(url)
        hls.attachMedia(video)
      } else {
        video.src = url
      }

      const progress = await getProgress(id!)
      if (progress.positionSeconds > 0) {
        video.currentTime = progress.positionSeconds
      }

      video.play().catch(() => {
        /* autoplay can be blocked by the browser; user can press play manually */
      })

      if (progressTimer) clearInterval(progressTimer)
      progressTimer = setInterval(() => {
        if (video.duration > 0) {
          updateProgress(id!, video.currentTime, video.duration).catch(() => {})
        }
      }, PROGRESS_REPORT_INTERVAL_MS)
    }

    // Shared by both the initial setup() and the mid-stream recovery path:
    // given a fresh playItem() response, either enter the acquiring
    // overlay+poll (the backend decided the link is dead and is fetching a
    // replacement) or attach and keep playing. Reaching here at all means
    // the backend gave a real, current answer, so this is also where the
    // retry budget resets.
    async function handlePlayResponse(video: HTMLVideoElement, play: PlayResponse) {
      midStreamRetries = 0
      if (play.mode === 'acquiring') {
        setAcquiring({ status: play.acquisitionStatus ?? 'searching', message: play.acquisitionError })
        if (play.acquisitionStatus !== 'error') pollAcquisition(video)
        return
      }
      setAcquiring(null)
      await attach(video, play)
    }

    // Fired on a native <video> network/decode/source error (direct mode)
    // or an hls.js fatal error (transcode mode) -- both mean the current
    // stream died, which for a debrid-backed item is most likely an
    // expired CDN link. Re-running playItem hands it back to the same
    // ffprobe-based liveness check handlePlayItem already does on a fresh
    // play click: if the link still works this reattaches seamlessly, and
    // if it's genuinely dead the backend starts replacing it and this page
    // shows the same acquiring overlay a first-time acquisition would.
    async function recoverFromPlaybackError() {
      if (cancelled || recovering) return
      const video = videoRef.current
      if (!video) return
      if (midStreamRetries >= MAX_MIDSTREAM_RETRIES) {
        setAcquiring({
          status: 'error',
          message: 'Playback was interrupted and could not recover automatically.',
        })
        return
      }
      recovering = true
      midStreamRetries += 1
      try {
        hlsRef.current?.destroy()
        hlsRef.current = null
        if (progressTimer) clearInterval(progressTimer)
        const play = await playItem(id!)
        if (cancelled) return
        await handlePlayResponse(video, play)
      } catch (err) {
        if (!cancelled) {
          setAcquiring({
            status: 'error',
            message: err instanceof ApiError ? err.message : 'Playback was interrupted and could not recover automatically.',
          })
        }
      } finally {
        recovering = false
      }
    }

    // Polls the item itself rather than a dedicated status endpoint --
    // GET /api/items/{id} already carries acquisitionStatus/acquisitionError
    // and is cheap, so a separate endpoint would just duplicate this.
    function pollAcquisition(video: HTMLVideoElement) {
      acquisitionTimer = setInterval(async () => {
        if (cancelled) return
        try {
          const fresh = await getItem(id!)
          if (cancelled) return
          if (fresh.acquisitionStatus === 'owned') {
            clearInterval(acquisitionTimer)
            const play = await playItem(id!)
            if (!cancelled) await handlePlayResponse(video, play)
          } else if (fresh.acquisitionStatus === 'error') {
            clearInterval(acquisitionTimer)
            setAcquiring({ status: 'error', message: fresh.acquisitionError || 'Acquisition failed.' })
          } else {
            setAcquiring({ status: fresh.acquisitionStatus as 'searching' | 'acquiring' })
          }
        } catch {
          /* transient poll failure: try again next tick */
        }
      }, ACQUISITION_POLL_INTERVAL_MS)
    }

    async function setup(video: HTMLVideoElement) {
      try {
        const loadedItem = await getItem(id!)
        if (cancelled) return
        setItem(loadedItem)
        findNextEpisode(loadedItem).then((n) => !cancelled && setNextEpisode(n))

        const play = await playItem(id!)
        if (cancelled) return
        await handlePlayResponse(video, play)
      } catch (err) {
        if (!cancelled) setError(err instanceof ApiError ? err.message : String(err))
      }
    }

    // MEDIA_ERR_ABORTED (code 1) fires on legitimate navigation/unmount --
    // the `cancelled` check inside recoverFromPlaybackError already guards
    // that case, but excluding it here too avoids even attempting recovery
    // while, say, the user is mid-navigation away from the page.
    function handleVideoError() {
      if (video!.error && video!.error.code !== video!.error.MEDIA_ERR_ABORTED) {
        recoverFromPlaybackError()
      }
    }
    video.addEventListener('error', handleVideoError)

    setup(video)

    return () => {
      cancelled = true
      video.removeEventListener('error', handleVideoError)
      if (progressTimer) clearInterval(progressTimer)
      if (acquisitionTimer) clearInterval(acquisitionTimer)
      hlsRef.current?.destroy()
      hlsRef.current = null
      if (sessionIdRef.current) {
        stopStreamSession(sessionIdRef.current).catch(() => {})
        sessionIdRef.current = null
      }
      if (video.duration > 0) {
        updateProgress(id!, video.currentTime, video.duration).catch(() => {})
      }
    }
  }, [id, retryKey])

  function handleTimeUpdate() {
    const video = videoRef.current
    if (!video || !nextEpisode || video.duration <= 0) return
    setShowUpNext(video.duration - video.currentTime <= NEAR_END_THRESHOLD_SECONDS)
  }

  function goToNextEpisode() {
    if (nextEpisode) navigate(`/watch/${nextEpisode.id}`, { replace: true })
  }

  function handleRetryAcquisition() {
    setError(null)
    setAcquiring(null)
    setRetryKey((k) => k + 1)
  }

  if (error) return <p className="vorn-form-error">{error}</p>

  return (
    <div className="vorn-watch-page">
      <div className="vorn-video-wrap">
        <video
          ref={videoRef}
          className="vorn-video"
          controls={!acquiring}
          onTimeUpdate={handleTimeUpdate}
          onEnded={goToNextEpisode}
        >
          {id && subtitleLanguage !== 'off' && (
            <track key={subtitleLanguage} kind="subtitles" src={subtitlesUrl(id, subtitleLanguage)} srcLang={subtitleLanguage} default />
          )}
        </video>

        {acquiring && (
          <div className="vorn-acquiring-overlay">
            {acquiring.status === 'error' ? (
              <>
                <p>{acquiring.message}</p>
                <button type="button" onClick={handleRetryAcquisition}>
                  Retry
                </button>
              </>
            ) : (
              <>
                <span className="vorn-acquiring-spinner" aria-hidden="true" />
                <p>{ACQUIRING_COPY[acquiring.status]}</p>
              </>
            )}
          </div>
        )}

        {showUpNext && nextEpisode && (
          <div className="vorn-up-next">
            <span>Up next: {nextEpisode.title}</span>
            <button type="button" onClick={goToNextEpisode}>
              Play now
            </button>
            <button type="button" onClick={() => setShowUpNext(false)}>
              Dismiss
            </button>
          </div>
        )}
      </div>
      {item && <h1 className="vorn-watch-title">{item.title}</h1>}

      <label className="vorn-subtitle-picker">
        Subtitles:{' '}
        <select value={subtitleLanguage} onChange={(e) => setSubtitleLanguage(e.target.value)}>
          {SUBTITLE_LANGUAGES.map((l) => (
            <option key={l.code} value={l.code}>
              {l.label}
            </option>
          ))}
        </select>
      </label>
    </div>
  )
}

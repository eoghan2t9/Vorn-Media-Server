import Hls from 'hls.js'
import 'plyr/dist/plyr.css'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  API_BASE,
  ApiError,
  beaconUpdateProgress,
  getItem,
  getProgress,
  playItem,
  subtitlesUrl,
  stopStreamSession,
  updateProgress,
  type AudioTrack,
  type MediaItem,
  type PlayResponse,
} from '../api/client'
import { Select } from '../components/Select'
import { detectClientCapabilities } from '../player/capabilities'
import { findNextEpisode } from '../player/nextEpisode'
import { usePlyr } from '../player/usePlyr'
import './WatchPage.css'

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

const PROGRESS_REPORT_INTERVAL_MS = 5000
// How long to wait before a single, non-chained retry of a failed periodic
// progress report -- short enough to meaningfully shrink the unsaved-progress
// window after a transient blip, without turning into its own retry storm.
const PROGRESS_RETRY_DELAY_MS = 2000
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
  const [audioTracks, setAudioTracks] = useState<AudioTrack[]>([])
  const [audioTrackIndex, setAudioTrackIndexState] = useState<number | undefined>(undefined)
  // Mirrors audioTrackIndex so the playback effect below (scoped to
  // [id, retryKey], not audio-track changes) can always read the current
  // selection when a mid-stream recovery re-requests playItem, without
  // needing to tear down and remount the whole session just because the
  // viewer picked a different track.
  const audioTrackIndexRef = useRef<number | undefined>(undefined)
  const switchAudioTrackRef = useRef<(index: number) => void>(() => {})

  function setAudioTrackIndex(index: number | undefined) {
    audioTrackIndexRef.current = index
    setAudioTrackIndexState(index)
  }

  useEffect(() => {
    if (!id) return
    const video = videoRef.current
    if (!video) return

    let cancelled = false
    let progressTimer: ReturnType<typeof setInterval> | undefined
    let progressRetryTimer: ReturnType<typeof setTimeout> | undefined
    let progressInFlight = false
    let acquisitionTimer: ReturnType<typeof setInterval> | undefined
    // Recovery budget for THIS mount/retry cycle -- reset to 0 on every
    // successful handlePlayResponse (see below) rather than only once per
    // page load, so a couple of early blips don't burn through the whole
    // budget and leave a two-hour movie with no recovery left for later.
    let midStreamRetries = 0
    let recovering = false

    // Serialized (progressInFlight) so a slow connection never has two
    // updates in the air at once -- without that, a request sent later
    // (newer position) can resolve before one sent earlier (older
    // position), and the older one landing last in UpsertPlaybackState would
    // silently overwrite a newer position with a stale one. On failure, one
    // bounded quick retry with a freshly-read position closes most of the
    // gap a transient blip would otherwise leave until the next periodic
    // tick; it isn't chained further; a connection that's still bad after
    // that just waits for the regular interval, same as before.
    async function reportProgress(video: HTMLVideoElement) {
      if (progressInFlight || video.duration <= 0) return
      progressInFlight = true
      try {
        await updateProgress(id!, video.currentTime, video.duration)
      } catch {
        if (cancelled) return
        await new Promise<void>((resolve) => {
          progressRetryTimer = setTimeout(resolve, PROGRESS_RETRY_DELAY_MS)
        })
        if (cancelled) return
        await updateProgress(id!, video.currentTime, video.duration).catch(() => {})
      } finally {
        progressInFlight = false
      }
    }

    // resumeAt overrides the server-reported saved position -- used when
    // reattaching right after the viewer picks a different audio track,
    // where we want to resume from wherever they actually were, not the
    // last periodically-saved progress (which can lag behind by up to
    // PROGRESS_REPORT_INTERVAL_MS).
    async function attach(video: HTMLVideoElement, play: PlayResponse, resumeAt?: number) {
      if (play.sessionId) sessionIdRef.current = play.sessionId
      // progressive (see transcode.ModeRemux) is played exactly like direct
      // -- plain video.src, no hls.js -- it's just a possibly-still-growing
      // MP4 file rather than a pre-existing URL.
      const url =
        play.mode === 'direct'
          ? `${API_BASE}${play.directUrl}`
          : play.mode === 'progressive'
            ? `${API_BASE}${play.progressiveUrl}`
            : `${API_BASE}${play.playlistUrl}`

      // Store the backend-probed duration on the video element as a
      // data attribute so the Plyr player controls can display the total
      // runtime even when the browser <video> cannot detect it from the
      // stream URL alone (e.g. debrid CDN redirects or growing files).
      if (play.durationSeconds && play.durationSeconds > 0) {
        video.dataset.duration = String(play.durationSeconds)
      }

      if (play.mode === 'transcode' && Hls.isSupported()) {
        // The API and the frontend are on different origins/ports (see
        // API_BASE) -- hls.js's default XHR loader doesn't send the
        // session cookie cross-origin unless told to, so every playlist/
        // segment fetch 401s and playback never starts. withCredentials
        // mirrors the credentials:'include' the rest of client.ts already
        // sets on every fetch() call.
        //
        // liveDurationInfinity: the backend's playlist has no ENDLIST until
        // ffmpeg finishes remuxing/transcoding the *entire* file (session.go
        // uses "-hls_playlist_type event", which only gets ENDLIST at the
        // very end) -- until then hls.js treats it as a live stream, and
        // without this flag it repeatedly shrinks/re-clamps the
        // MediaSource's reported duration on every playlist refresh as the
        // known-so-far total changes, which stalls playback right at the
        // (stale) duration boundary until the next refresh. That's what
        // "press play, inches forward a frame, stalls" was -- forcing
        // duration to Infinity while it's still growing avoids the
        // recurring clamp entirely; a real (non-live) duration still shows
        // once the playlist is complete.
        const hls = new Hls({ xhrSetup: (xhr) => { xhr.withCredentials = true }, liveDurationInfinity: true })
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

      if (resumeAt !== undefined) {
        video.currentTime = resumeAt
      } else {
        const progress = await getProgress(id!)
        if (progress.positionSeconds > 0) {
          video.currentTime = progress.positionSeconds
        }
      }

      video.play().catch(() => {
        /* autoplay can be blocked by the browser; user can press play manually */
      })

      if (progressTimer) clearInterval(progressTimer)
      progressTimer = setInterval(() => reportProgress(video), PROGRESS_REPORT_INTERVAL_MS)
    }

    // Shared by both the initial setup() and the mid-stream recovery path:
    // given a fresh playItem() response, either enter the acquiring
    // overlay+poll (the backend decided the link is dead and is fetching a
    // replacement) or attach and keep playing. Reaching here at all means
    // the backend gave a real, current answer, so this is also where the
    // retry budget resets.
    async function handlePlayResponse(video: HTMLVideoElement, play: PlayResponse, resumeAt?: number) {
      midStreamRetries = 0
      setAudioTracks(play.audioTracks ?? [])
      if (play.mode === 'acquiring') {
        setAcquiring({ status: play.acquisitionStatus ?? 'searching', message: play.acquisitionError })
        if (play.acquisitionStatus !== 'error') pollAcquisition(video)
        return
      }
      setAcquiring(null)
      await attach(video, play, resumeAt)
    }

    // Re-requests playback with a different audio track, preserving the
    // current position -- exposed to the render via switchAudioTrackRef
    // since this playback session lives inside this effect, scoped to
    // [id, retryKey], while the settings-menu click that triggers it comes
    // from outside.
    async function switchAudioTrack(index: number) {
      const video = videoRef.current
      if (!video || cancelled) return
      const resumeAt = video.currentTime
      try {
        hlsRef.current?.destroy()
        hlsRef.current = null
        if (progressTimer) clearInterval(progressTimer)
        const play = await playItem(id!, { caps: detectClientCapabilities(), audioTrackIndex: index })
        if (cancelled) return
        setAudioTrackIndex(index)
        await handlePlayResponse(video, play, resumeAt)
      } catch (err) {
        if (!cancelled) {
          setAcquiring({
            status: 'error',
            message: err instanceof ApiError ? err.message : 'Failed to switch audio track.',
          })
        }
      }
    }
    switchAudioTrackRef.current = switchAudioTrack

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
        const play = await playItem(id!, { caps: detectClientCapabilities(), audioTrackIndex: audioTrackIndexRef.current })
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
            const play = await playItem(id!, { caps: detectClientCapabilities(), audioTrackIndex: audioTrackIndexRef.current })
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

        const play = await playItem(id!, { caps: detectClientCapabilities(), audioTrackIndex: audioTrackIndexRef.current })
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

    // A component-unmount cleanup (SPA navigation to another route) still
    // runs in a live JS context, so the regular fetch-based updateProgress
    // in the cleanup below is reliable for that case. A tab close, hard
    // navigation, or refresh tears the page down before an in-flight fetch
    // can finish, and never runs React's cleanup at all -- pagehide is the
    // standard hook for that moment, which is why the save there goes
    // through sendBeacon instead. visibilitychange->hidden is a second,
    // earlier-firing safety net for mobile browsers backgrounding the tab
    // without a prompt pagehide.
    function saveProgressViaBeacon() {
      if (video!.duration > 0) {
        beaconUpdateProgress(id!, video!.currentTime, video!.duration)
      }
    }
    function handleVisibilityChange() {
      if (document.visibilityState === 'hidden') saveProgressViaBeacon()
    }
    window.addEventListener('pagehide', saveProgressViaBeacon)
    document.addEventListener('visibilitychange', handleVisibilityChange)

    setup(video)

    return () => {
      cancelled = true
      video.removeEventListener('error', handleVideoError)
      window.removeEventListener('pagehide', saveProgressViaBeacon)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      if (progressTimer) clearInterval(progressTimer)
      if (progressRetryTimer) clearTimeout(progressRetryTimer)
      if (acquisitionTimer) clearInterval(acquisitionTimer)
      hlsRef.current?.destroy()
      hlsRef.current = null
      if (sessionIdRef.current) {
        stopStreamSession(sessionIdRef.current).catch(() => {})
        sessionIdRef.current = null
      }
      saveProgressViaBeacon()
    }
  }, [id, retryKey])

  usePlyr(videoRef)

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
        <video ref={videoRef} className="vorn-video" playsInline onTimeUpdate={handleTimeUpdate} onEnded={goToNextEpisode}>
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

      <div className="vorn-watch-track-pickers">
        {audioTracks.length > 1 && (
          <label className="vorn-watch-track-picker">
            Audio
            <Select
              value={String(audioTrackIndex ?? audioTracks[0]?.index ?? 0)}
              onChange={(v) => switchAudioTrackRef.current(Number(v))}
              options={audioTracks.map((t) => ({
                value: String(t.index),
                label: `${t.title || t.language || t.codec}${t.channels ? ` (${t.channels}ch)` : ''}`,
              }))}
            />
          </label>
        )}
        <label className="vorn-watch-track-picker">
          Subtitles
          <Select value={subtitleLanguage} onChange={setSubtitleLanguage} options={SUBTITLE_LANGUAGES.map((l) => ({ value: l.code, label: l.label }))} />
        </label>
      </div>
    </div>
  )
}

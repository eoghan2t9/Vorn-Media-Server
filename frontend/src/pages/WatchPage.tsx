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
} from '../api/client'
import { findNextEpisode } from '../player/nextEpisode'
import './WatchPage.css'

const PROGRESS_REPORT_INTERVAL_MS = 5000
const NEAR_END_THRESHOLD_SECONDS = 30

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

  useEffect(() => {
    if (!id) return
    const video = videoRef.current
    if (!video) return

    let cancelled = false
    let progressTimer: ReturnType<typeof setInterval> | undefined

    async function setup(video: HTMLVideoElement) {
      try {
        const loadedItem = await getItem(id!)
        if (cancelled) return
        setItem(loadedItem)
        findNextEpisode(loadedItem).then((n) => !cancelled && setNextEpisode(n))

        const play = await playItem(id!)
        if (cancelled) return
        if (play.sessionId) sessionIdRef.current = play.sessionId

        const url = play.mode === 'direct' ? `${API_BASE}${play.directUrl}` : `${API_BASE}${play.playlistUrl}`

        if (play.mode === 'transcode' && Hls.isSupported()) {
          const hls = new Hls()
          hlsRef.current = hls
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

        progressTimer = setInterval(() => {
          if (video.duration > 0) {
            updateProgress(id!, video.currentTime, video.duration).catch(() => {})
          }
        }, PROGRESS_REPORT_INTERVAL_MS)
      } catch (err) {
        if (!cancelled) setError(err instanceof ApiError ? err.message : String(err))
      }
    }

    setup(video)

    return () => {
      cancelled = true
      if (progressTimer) clearInterval(progressTimer)
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
  }, [id])

  function handleTimeUpdate() {
    const video = videoRef.current
    if (!video || !nextEpisode || video.duration <= 0) return
    setShowUpNext(video.duration - video.currentTime <= NEAR_END_THRESHOLD_SECONDS)
  }

  function goToNextEpisode() {
    if (nextEpisode) navigate(`/watch/${nextEpisode.id}`, { replace: true })
  }

  if (error) return <p className="vorn-form-error">{error}</p>

  return (
    <div className="vorn-watch-page">
      <video
        ref={videoRef}
        className="vorn-video"
        controls
        onTimeUpdate={handleTimeUpdate}
        onEnded={goToNextEpisode}
      >
        {id && subtitleLanguage !== 'off' && (
          <track key={subtitleLanguage} kind="subtitles" src={subtitlesUrl(id, subtitleLanguage)} srcLang={subtitleLanguage} default />
        )}
      </video>
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
  )
}

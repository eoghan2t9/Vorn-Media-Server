import { useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { API_BASE, listTorrents, type Torrent } from '../api/client'
import { usePlyr } from '../player/usePlyr'
import './WatchPage.css'

// TorrentStreamPage plays a still-downloading local torrent directly --
// unlike WatchPage, this never touches media_items/playItem's probe/
// transcode-decide negotiation. The backend endpoint
// (GET /api/torrents/{id}/stream) only ever serves the source bytes
// as-is, so this only works for browser-natively-playable sources (same
// ceiling as WatchPage's own ModeDirect case) -- there is no transcode
// fallback for a torrent that isn't done downloading yet.
export function TorrentStreamPage() {
  const { id } = useParams<{ id: string }>()
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const [torrent, setTorrent] = useState<Torrent | null>(null)
  const [error, setError] = useState<string | null>(null)

  usePlyr(videoRef)

  useEffect(() => {
    if (!id) return
    let cancelled = false
    listTorrents()
      .then((all) => {
        if (cancelled) return
        const found = all.find((t) => t.id === id)
        if (!found) {
          setError('Torrent not found')
          return
        }
        setTorrent(found)
      })
      .catch(() => {
        if (!cancelled) setError('Failed to load torrent')
      })
    return () => {
      cancelled = true
    }
  }, [id])

  if (!id) return null

  return (
    <div className="vorn-watch-page">
      <div className="vorn-video-wrap">
        <video
          ref={videoRef}
          className="vorn-video"
          playsInline
          controls
          src={`${API_BASE}/api/torrents/${id}/stream`}
          onError={() => setError('Playback failed -- this source may need transcoding, which is only available once the torrent finishes downloading.')}
        />
      </div>
      {torrent && <h1 className="vorn-watch-title">{torrent.name || torrent.infoHash}</h1>}
      {error && <p style={{ color: '#e5484d', padding: '0 1rem' }}>{error}</p>}
    </div>
  )
}

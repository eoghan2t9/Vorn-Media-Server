import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  ApiError,
  getItem,
  openCatalogEntry,
  resolveMediaUrl,
  setItemMonitored,
  updateItemMetadata,
  type CatalogEntry,
  type MediaItemDetail,
} from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { CastRow } from '../components/CastRow'
import { Poster } from '../components/Poster'
import { RatingBadge } from '../components/RatingBadge'
import './ItemDetail.css'

function EditMetadataForm({ item, onSaved }: { item: MediaItemDetail; onSaved: (updated: MediaItemDetail) => void }) {
  const [title, setTitle] = useState(item.title)
  const [overview, setOverview] = useState(item.overview ?? '')
  const [releaseDate, setReleaseDate] = useState(item.releaseDate ?? '')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSaving(true)
    try {
      const updated = await updateItemMetadata(item.id, { title, overview, releaseDate: releaseDate || undefined })
      onSaved({ ...item, ...updated })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save metadata')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form className="vorn-form vorn-metadata-edit" onSubmit={handleSubmit}>
      <h2>Correct metadata</h2>
      <p className="vorn-form-subtitle">
        Use this if the item was misidentified. Saving locks it so a future metadata sync won't overwrite the fix.
      </p>
      <label>
        Title
        <input value={title} onChange={(e) => setTitle(e.target.value)} required />
      </label>
      <label>
        Overview
        <textarea value={overview} onChange={(e) => setOverview(e.target.value)} rows={4} />
      </label>
      <label>
        Release date
        <input type="date" value={releaseDate} onChange={(e) => setReleaseDate(e.target.value)} />
      </label>
      {error && <p className="vorn-form-error">{error}</p>}
      <button type="submit" disabled={saving}>
        {saving ? 'Saving…' : 'Save correction'}
      </button>
    </form>
  )
}

export function ItemDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { user } = useAuth()
  const [item, setItem] = useState<MediaItemDetail | null>(null)
  const [editing, setEditing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [togglingMonitor, setTogglingMonitor] = useState(false)
  const [openingSimilarId, setOpeningSimilarId] = useState<number | null>(null)

  useEffect(() => {
    if (!id) return
    getItem(id)
      .then(setItem)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [id])

  async function handleToggleMonitor() {
    if (!item) return
    setTogglingMonitor(true)
    try {
      const updated = await setItemMonitored(item.id, !item.monitored)
      setItem({ ...item, ...updated })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to update monitoring')
    } finally {
      setTogglingMonitor(false)
    }
  }

  // A similar-title card has no local media_item yet -- materialize it into
  // this item's own library (same mechanism as BrowsePage's handleOpen) and
  // jump straight to its page. mediaType is 'series' for both series and
  // episode, since an inherited similar-list is always TV titles there.
  async function handleOpenSimilar(entry: CatalogEntry) {
    if (!item) return
    setError(null)
    setOpeningSimilarId(entry.tmdbId)
    try {
      const opened = await openCatalogEntry({
        tmdbId: entry.tmdbId,
        mediaType: item.kind === 'movie' ? 'movie' : 'series',
        libraryId: item.libraryId,
      })
      navigate(`/items/${opened.id}`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
      setOpeningSimilarId(null)
    }
  }

  if (error) return <p className="vorn-form-error">{error}</p>
  if (!item) return <p>Loading…</p>

  return (
    <section>
      <div
        className="vorn-detail-hero"
        style={item.backdropUrl ? { backgroundImage: `url(${resolveMediaUrl(item.backdropUrl)})` } : undefined}
      >
        <div className="vorn-detail-hero-scrim" />
        <div className="vorn-detail-hero-content">
          <Poster title={item.title} posterUrl={item.posterUrl} className="vorn-detail-poster" />
          <div className="vorn-detail-info">
            {item.logoUrl ? (
              <img src={resolveMediaUrl(item.logoUrl)} alt={item.title} className="vorn-detail-logo" />
            ) : (
              <h1>{item.title}</h1>
            )}
            {item.author && <p className="vorn-detail-year">by {item.author}</p>}
            {item.releaseDate && <p className="vorn-detail-year">{item.releaseDate.slice(0, 4)}</p>}
            {(item.ratingImdb || item.ratingRottenTomatoes || item.ratingTmdb) && (
              <p className="vorn-detail-ratings">
                {item.ratingTmdb && <span>TMDb {item.ratingTmdb}</span>}
                {item.ratingImdb && <span>IMDb {item.ratingImdb}</span>}
                {item.ratingRottenTomatoes && <span>RT {item.ratingRottenTomatoes}</span>}
              </p>
            )}
            {(item.kind === 'movie' ||
              item.kind === 'episode' ||
              item.kind === 'track' ||
              item.kind === 'audiobook' ||
              item.kind === 'chapter') && (
              <Link to={`/watch/${item.id}`} className="vorn-play-button">
                ▶ Play
              </Link>
            )}
            {item.trailerUrl && (
              <a href={item.trailerUrl} target="_blank" rel="noopener noreferrer" className="vorn-trailer-button">
                ▶ Watch Trailer
              </a>
            )}
            {(item.kind === 'movie' || item.kind === 'series') && (
              <button
                type="button"
                className="vorn-edit-toggle"
                onClick={handleToggleMonitor}
                disabled={togglingMonitor}
                title="Automatically grab new episodes / retry until available, and upgrade quality once owned"
              >
                {item.monitored ? '★ Monitored' : '☆ Monitor'}
              </button>
            )}
          </div>
        </div>
      </div>

      {item.overview && <p className="vorn-detail-overview">{item.overview}</p>}
      {item.currentReleaseTitle && (
        <p className="vorn-detail-year" style={{ opacity: 0.7 }}>
          Current: {item.currentReleaseTitle}
        </p>
      )}

      <CastRow cast={item.cast ?? []} directors={item.directors} />

      {item.children && item.children.length > 0 && (
        <div className="vorn-children-row">
          {item.children.map((c) => (
            <Link to={`/items/${c.id}`} key={c.id} className="vorn-child-card">
              <Poster title={c.title} posterUrl={c.posterUrl}>
                <RatingBadge rating={c.ratingTmdb} />
              </Poster>
              <div className="vorn-child-title">{c.title}</div>
            </Link>
          ))}
        </div>
      )}

      {item.similar && item.similar.length > 0 && (
        <div className="vorn-children-row">
          {item.similar.map((sm) => (
            <button
              type="button"
              key={sm.tmdbId}
              className="vorn-child-card vorn-similar-card"
              onClick={() => handleOpenSimilar(sm)}
              disabled={openingSimilarId === sm.tmdbId}
            >
              <Poster title={sm.title} posterUrl={sm.posterUrl}>
                <RatingBadge rating={sm.rating} />
              </Poster>
              <div className="vorn-child-title">{openingSimilarId === sm.tmdbId ? 'Opening…' : sm.title}</div>
            </button>
          ))}
        </div>
      )}

      {user?.isAdmin && (item.kind === 'movie' || item.kind === 'series') && (
        <>
          {editing ? (
            <EditMetadataForm
              item={item}
              onSaved={(updated) => {
                setItem(updated)
                setEditing(false)
              }}
            />
          ) : (
            <button type="button" className="vorn-edit-toggle" onClick={() => setEditing(true)}>
              Edit metadata
            </button>
          )}
        </>
      )}
    </section>
  )
}

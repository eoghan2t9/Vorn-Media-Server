import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  ApiError,
  listContinueWatching,
  listLibraries,
  listLibraryItems,
  type ContinueWatchingEntry,
  type Library,
  type MediaItem,
} from '../api/client'
import { Poster } from '../components/Poster'
import { RatingBadge } from '../components/RatingBadge'
import './ViewerHome.css'

// Home only ever shows a "recently added" taste of each library -- the
// dedicated per-library page (LibraryPage, /libraries/:id) is where the
// full catalog with sorting/paging actually lives.
const PREVIEW_COUNT = 12

function LibraryRow({ library }: { library: Library }) {
  const [items, setItems] = useState<MediaItem[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    listLibraryItems(library.id, { sort: 'recent' })
      .then((all) => setItems(all.slice(0, PREVIEW_COUNT)))
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [library.id])

  return (
    <section className="vorn-library-row">
      <div className="vorn-library-row-header">
        <h2>
          <Link to={`/libraries/${library.id}`}>{library.name}</Link>
          {library.is4K && (
            <span className="vorn-user-badge" style={{ marginLeft: '0.5rem' }} title="4K-only library">
              4K
            </span>
          )}
        </h2>
        <Link to={`/libraries/${library.id}`} className="vorn-library-row-viewall">
          View all →
        </Link>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}
      {items.length === 0 ? (
        <p className="vorn-empty">Nothing here yet — scan this library from the admin area.</p>
      ) : (
        <div className="vorn-card-grid">
          {items.map((item) => (
            <Link to={`/items/${item.id}`} key={item.id} className="vorn-card">
              <Poster title={item.title} posterUrl={item.posterUrl}>
                <RatingBadge rating={item.ratingTmdb} />
              </Poster>
              <div className="vorn-card-title">{item.title}</div>
              {item.releaseDate && <div className="vorn-card-meta">{item.releaseDate.slice(0, 4)}</div>}
            </Link>
          ))}
        </div>
      )}
    </section>
  )
}

function ContinueWatchingRow({ entries }: { entries: ContinueWatchingEntry[] }) {
  if (entries.length === 0) return null
  return (
    <section className="vorn-library-row">
      <div className="vorn-library-row-header">
        <h2>Continue Watching</h2>
      </div>
      <div className="vorn-card-grid">
        {entries.map((e) => {
          const pct = e.durationSeconds > 0 ? (e.positionSeconds / e.durationSeconds) * 100 : 0
          return (
            <Link to={`/items/${e.item.id}`} key={e.item.id} className="vorn-card">
              <Poster title={e.item.title} posterUrl={e.item.posterUrl}>
                <RatingBadge rating={e.item.ratingTmdb} />
                <div className="vorn-progress-bar">
                  <div className="vorn-progress-fill" style={{ width: `${pct}%` }} />
                </div>
              </Poster>
              <div className="vorn-card-title">{e.item.title}</div>
            </Link>
          )
        })}
      </div>
    </section>
  )
}

export function ViewerHome() {
  const [libraries, setLibraries] = useState<Library[]>([])
  const [continueWatching, setContinueWatching] = useState<ContinueWatchingEntry[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    Promise.all([listLibraries(), listContinueWatching()])
      .then(([libs, cw]) => {
        setLibraries(libs)
        setContinueWatching(cw)
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  if (error) return <p className="vorn-form-error">{error}</p>

  return (
    <div>
      <ContinueWatchingRow entries={continueWatching} />
      {libraries.length === 0 ? (
        <p className="vorn-empty">No libraries yet. An admin needs to add one.</p>
      ) : (
        libraries.map((lib) => <LibraryRow key={lib.id} library={lib} />)
      )}
    </div>
  )
}

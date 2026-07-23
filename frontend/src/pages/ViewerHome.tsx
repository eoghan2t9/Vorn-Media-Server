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
import './ViewerHome.css'

type SortMode = 'recent' | 'alpha'

function LibraryRow({ library }: { library: Library }) {
  const [items, setItems] = useState<MediaItem[]>([])
  const [sort, setSort] = useState<SortMode>('recent')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    listLibraryItems(library.id, { sort })
      .then(setItems)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [library.id, sort])

  return (
    <section className="vorn-library-row">
      <div className="vorn-library-row-header">
        <h2>{library.name}</h2>
        <select value={sort} onChange={(e) => setSort(e.target.value as SortMode)}>
          <option value="recent">Recently added</option>
          <option value="alpha">A–Z</option>
        </select>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}
      {items.length === 0 ? (
        <p className="vorn-empty">Nothing here yet — scan this library from the admin area.</p>
      ) : (
        <div className="vorn-card-grid">
          {items.map((item) => (
            <Link to={`/items/${item.id}`} key={item.id} className="vorn-card">
              <div className="vorn-card-poster" aria-hidden />
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
              <div className="vorn-card-poster" aria-hidden>
                <div className="vorn-progress-bar">
                  <div className="vorn-progress-fill" style={{ width: `${pct}%` }} />
                </div>
              </div>
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

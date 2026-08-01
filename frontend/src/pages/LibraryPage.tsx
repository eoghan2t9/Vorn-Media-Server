import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, getLibrary, listLibraryItems, setItemMonitored, type Library, type MediaItem } from '../api/client'
import { Pagination } from '../components/Pagination'
import { Poster } from '../components/Poster'
import { RatingBadge } from '../components/RatingBadge'
import { Select } from '../components/Select'
import { usePagination } from '../components/usePagination'
import './ViewerHome.css'

type SortMode = 'recent' | 'alpha'
type MonitorFilter = 'all' | 'unmonitored'

// The full-catalog counterpart to Home's per-library preview row (see
// PREVIEW_COUNT in ViewerHome.tsx) -- this is where sorting and paging
// through everything in a single library actually lives.
export function LibraryPage() {
  const { id } = useParams<{ id: string }>()
  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<MediaItem[]>([])
  const [sort, setSort] = useState<SortMode>('alpha')
  const [monitorFilter, setMonitorFilter] = useState<MonitorFilter>('all')
  const [monitoringAll, setMonitoringAll] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Apply client-side monitor filter.
  const filtered = monitorFilter === 'all'
    ? items
    : items.filter((i) => i.acquisitionStatus === 'owned' && (i.kind === 'movie' || i.kind === 'series') && !i.monitored)
  const itemsPage = usePagination(filtered, 24)

  useEffect(() => {
    if (!id) return
    getLibrary(id)
      .then(setLibrary)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [id])

  useEffect(() => {
    if (!id) return
    listLibraryItems(id, { sort })
      .then(setItems)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [id, sort])

  // Changing sort order or filter reshuffles which items land on which
  // page -- staying on, say, page 3 after switching would show an
  // unrelated, likely truncated slice of the library.
  useEffect(() => itemsPage.setPage(1), [sort, monitorFilter])

  const handleMonitorAll = async () => {
    setMonitoringAll(true)
    try {
      const owned = items.filter(
        (i) => i.acquisitionStatus === 'owned' && (i.kind === 'movie' || i.kind === 'series') && !i.monitored,
      )
      for (const item of owned) {
        try {
          const updated = await setItemMonitored(item.id, true)
          setItems((prev) => prev.map((i) => (i.id === updated.id ? updated : i)))
        } catch {
          // continue trying other items
        }
      }
    } finally {
      setMonitoringAll(false)
    }
  }

  return (
    <section className="vorn-library-row">
      <div className="vorn-library-row-header">
        <h1>
          {library?.name ?? 'Library'}
          {library?.is4K && (
            <span className="vorn-user-badge" style={{ marginLeft: '0.5rem' }} title="4K-only library">
              4K
            </span>
          )}
        </h1>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <Select
            value={monitorFilter}
            onChange={(v) => setMonitorFilter(v as MonitorFilter)}
            options={[
              { value: 'all', label: 'All' },
              { value: 'unmonitored', label: 'Unmonitored' },
            ]}
          />
          <Select
            value={sort}
            onChange={(v) => setSort(v as SortMode)}
            options={[
              { value: 'alpha', label: 'A–Z' },
              { value: 'recent', label: 'Recently added' },
            ]}
          />
          {monitorFilter === 'unmonitored' && (
            <button
              type="button"
              className="vorn-library-monitor-all"
              onClick={handleMonitorAll}
              disabled={monitoringAll}
            >
              {monitoringAll ? 'Monitoring…' : '★ Monitor All Unmonitored'}
            </button>
          )}
        </div>
      </div>

      {error && <p className="vorn-form-error">{error}</p>}

      {filtered.length === 0 ? (
        <p className="vorn-empty">
          {monitorFilter === 'unmonitored'
            ? 'All owned items are monitored.'
            : 'Nothing here yet — scan this library from the admin area.'}
        </p>
      ) : (
        <div className="vorn-card-grid">
          {itemsPage.pageItems.map((item) => (
            <Link to={`/items/${item.id}`} key={item.id} className="vorn-card">
              <Poster title={item.title} posterUrl={item.posterUrl}>
                <RatingBadge rating={item.ratingTmdb} />
              </Poster>
              <div className="vorn-card-title">{item.title}</div>
              {item.releaseDate && <div className="vorn-card-meta">{item.releaseDate.slice(0, 4)}</div>}
              {item.acquisitionStatus === 'owned' && (item.kind === 'movie' || item.kind === 'series') && (
                <span
                  className="vorn-card-monitor-btn"
                  onClick={async (e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    try {
                      const updated = await setItemMonitored(item.id, !item.monitored)
                      setItems((prev) => prev.map((i) => (i.id === updated.id ? updated : i)))
                    } catch {}
                  }}
                  title={item.monitored ? 'Stop watching for upgrades and new episodes' : 'Watch for better releases and new episodes'}
                >
                  {item.monitored ? '★ Monitored' : '☆ Monitor'}
                </span>
              )}
            </Link>
          ))}
        </div>
      )}

      <Pagination page={itemsPage.page} totalPages={itemsPage.totalPages} onChange={itemsPage.setPage} />
    </section>
  )
}

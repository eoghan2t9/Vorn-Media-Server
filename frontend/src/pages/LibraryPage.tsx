import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, getLibrary, listLibraryItems, type Library, type MediaItem } from '../api/client'
import { Pagination } from '../components/Pagination'
import { Poster } from '../components/Poster'
import { RatingBadge } from '../components/RatingBadge'
import { Select } from '../components/Select'
import { usePagination } from '../components/usePagination'
import './ViewerHome.css'

type SortMode = 'recent' | 'alpha'

// The full-catalog counterpart to Home's per-library preview row (see
// PREVIEW_COUNT in ViewerHome.tsx) -- this is where sorting and paging
// through everything in a single library actually lives.
export function LibraryPage() {
  const { id } = useParams<{ id: string }>()
  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<MediaItem[]>([])
  const [sort, setSort] = useState<SortMode>('alpha')
  const [error, setError] = useState<string | null>(null)
  const itemsPage = usePagination(items, 24)

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

  // Changing sort order effectively reshuffles which items land on which
  // page -- staying on, say, page 3 of "Recently added" after switching to
  // "A-Z" would show an unrelated, likely truncated slice of the library.
  useEffect(() => itemsPage.setPage(1), [sort])

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
        <Select
          value={sort}
          onChange={(v) => setSort(v as SortMode)}
          options={[
            { value: 'alpha', label: 'A–Z' },
            { value: 'recent', label: 'Recently added' },
          ]}
        />
      </div>

      {error && <p className="vorn-form-error">{error}</p>}

      {items.length === 0 ? (
        <p className="vorn-empty">Nothing here yet — scan this library from the admin area.</p>
      ) : (
        <div className="vorn-card-grid">
          {itemsPage.pageItems.map((item) => (
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

      <Pagination page={itemsPage.page} totalPages={itemsPage.totalPages} onChange={itemsPage.setPage} />
    </section>
  )
}

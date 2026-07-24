import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ApiError,
  listCatalog,
  listLibraries,
  openCatalogEntry,
  type CatalogEntry,
  type Library,
} from '../api/client'
import { Poster } from '../components/Poster'
import { Select } from '../components/Select'
import './ViewerHome.css'

type MediaType = 'movie' | 'series'
type SortMode = 'popular' | 'trending'

export function BrowsePage() {
  const navigate = useNavigate()
  const [mediaType, setMediaType] = useState<MediaType>('movie')
  const [sort, setSort] = useState<SortMode>('popular')
  const [libraries, setLibraries] = useState<Library[]>([])
  const [targetLibraryId, setTargetLibraryId] = useState('')
  const [results, setResults] = useState<CatalogEntry[]>([])
  const [page, setPage] = useState(1)
  const [totalPages, setTotalPages] = useState(1)
  const [loading, setLoading] = useState(false)
  const [openingId, setOpeningId] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const matchingLibraries = libraries.filter((l) => l.type === mediaType)
  // Defaults to the first library of the current media type -- the common
  // case (one movie library, one series library) never has to think about
  // this; the picker below only appears once a second library of the same
  // type exists (e.g. a "Movies 4K" library alongside "Movies"). Computed
  // during render rather than synced via an effect+extra state, since it's
  // a pure function of (libraries, mediaType, targetLibraryId) -- only an
  // explicit pick in the Select below ever calls setTargetLibraryId.
  const effectiveLibraryId = matchingLibraries.some((l) => l.id === targetLibraryId)
    ? targetLibraryId
    : (matchingLibraries[0]?.id ?? '')

  useEffect(() => {
    listLibraries()
      .then(setLibraries)
      .catch(() => {})
  }, [])

  useEffect(() => {
    setError(null)
    setLoading(true)
    listCatalog(mediaType, sort, 1)
      .then((p) => {
        setResults(p.results)
        setPage(p.page)
        setTotalPages(p.totalPages)
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
      .finally(() => setLoading(false))
  }, [mediaType, sort])

  async function loadMore() {
    setLoading(true)
    try {
      const next = await listCatalog(mediaType, sort, page + 1)
      setResults((r) => [...r, ...next.results])
      setPage(next.page)
      setTotalPages(next.totalPages)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  // A browse card has no local library of its own yet -- it's materialized
  // into whichever library the picker below currently targets (defaults to
  // the first of the matching type, so the common single-library case never
  // has to think about this).
  async function handleOpen(entry: CatalogEntry) {
    setError(null)
    if (!effectiveLibraryId) {
      setError(`No ${mediaType === 'movie' ? 'movie' : 'TV'} library configured yet -- ask an admin to add one first.`)
      return
    }
    setOpeningId(entry.tmdbId)
    try {
      const item = await openCatalogEntry({ tmdbId: entry.tmdbId, mediaType, libraryId: effectiveLibraryId })
      navigate(`/items/${item.id}`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
      setOpeningId(null)
    }
  }

  return (
    <div>
      <section className="vorn-library-row">
        <div className="vorn-library-row-header">
          <h2>Browse</h2>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <Select
              value={mediaType}
              onChange={(v) => setMediaType(v as MediaType)}
              options={[
                { value: 'movie', label: 'Movies' },
                { value: 'series', label: 'TV Shows' },
              ]}
            />
            <Select
              value={sort}
              onChange={(v) => setSort(v as SortMode)}
              options={[
                { value: 'popular', label: 'Popular' },
                { value: 'trending', label: 'Trending' },
              ]}
            />
            {matchingLibraries.length > 1 && (
              <Select
                value={effectiveLibraryId}
                onChange={setTargetLibraryId}
                options={matchingLibraries.map((l) => ({ value: l.id, label: l.name }))}
              />
            )}
          </div>
        </div>

        {error && <p className="vorn-form-error">{error}</p>}

        {results.length === 0 && !loading ? (
          <p className="vorn-empty">Nothing to show. Check that a TMDb API key is configured in Admin &gt; Integrations.</p>
        ) : (
          <div className="vorn-card-grid">
            {results.map((entry) => (
              <button
                type="button"
                key={entry.tmdbId}
                className="vorn-card"
                onClick={() => handleOpen(entry)}
                disabled={openingId === entry.tmdbId}
              >
                <Poster title={entry.title} posterUrl={entry.posterUrl} />
                <div className="vorn-card-title">{openingId === entry.tmdbId ? 'Opening…' : entry.title}</div>
                {entry.releaseDate && <div className="vorn-card-meta">{entry.releaseDate.slice(0, 4)}</div>}
              </button>
            ))}
          </div>
        )}

        {results.length > 0 && page < totalPages && (
          <div style={{ textAlign: 'center', marginTop: '1rem' }}>
            <button type="button" onClick={loadMore} disabled={loading}>
              {loading ? 'Loading…' : 'Load more'}
            </button>
          </div>
        )}
      </section>
    </div>
  )
}

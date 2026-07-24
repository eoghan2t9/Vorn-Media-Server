import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, createContentRequest, discoverSearch, search, type DiscoverResult, type SearchResult } from '../api/client'

type UnownedResult = DiscoverResult & { mediaType: 'movie' | 'series' }

export function HeaderSearch() {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [unowned, setUnowned] = useState<UnownedResult[]>([])
  const [requestedIds, setRequestedIds] = useState<Set<number>>(new Set())
  const [requestingId, setRequestingId] = useState<number | null>(null)
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()

  useEffect(() => {
    if (query.trim().length < 2) {
      setResults([])
      setUnowned([])
      return
    }
    const q = query.trim()
    const handle = setTimeout(() => {
      search(q)
        .then(async (items) => {
          setResults(items)
          setOpen(true)
          if (items.length > 0) {
            setUnowned([])
            return
          }
          // Nothing local -- fall back to TMDb so the user can request it
          // straight from search instead of hitting a dead end.
          try {
            const [movies, series] = await Promise.all([discoverSearch(q, 'movie'), discoverSearch(q, 'series')])
            setUnowned([
              ...movies.map((r) => ({ ...r, mediaType: 'movie' as const })),
              ...series.map((r) => ({ ...r, mediaType: 'series' as const })),
            ].slice(0, 6))
            setOpen(true)
          } catch {
            setUnowned([])
          }
        })
        .catch(() => {
          setResults([])
          setUnowned([])
        })
    }, 250)
    return () => clearTimeout(handle)
  }, [query])

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  function goToItem(id: string) {
    setOpen(false)
    setQuery('')
    navigate(`/items/${id}`)
  }

  async function handleRequest(r: UnownedResult) {
    setRequestingId(r.tmdbId)
    try {
      await createContentRequest({
        mediaType: r.mediaType,
        tmdbId: r.tmdbId,
        title: r.title,
        overview: r.overview,
        releaseDate: r.releaseDate,
        posterUrl: r.posterUrl,
      })
      setRequestedIds((ids) => new Set(ids).add(r.tmdbId))
    } catch (err) {
      // A 409 here just means someone already requested it -- still worth
      // reflecting as "requested" rather than surfacing an error.
      if (err instanceof ApiError && err.status === 409) {
        setRequestedIds((ids) => new Set(ids).add(r.tmdbId))
      }
    } finally {
      setRequestingId(null)
    }
  }

  return (
    <div className="vorn-header-search" ref={containerRef}>
      <input
        type="search"
        placeholder="Search your library…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onFocus={() => (results.length > 0 || unowned.length > 0) && setOpen(true)}
      />
      {open && results.length > 0 && (
        <ul className="vorn-search-results">
          {results.map((r) => (
            <li key={r.id}>
              <button type="button" onClick={() => goToItem(r.id)}>
                {r.title}
                <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                  <span className="vorn-search-kind">{r.kind}</span>
                  {r.is4K && (
                    <span className="vorn-user-badge" title="4K-only library">
                      4K
                    </span>
                  )}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {open && results.length === 0 && unowned.length > 0 && (
        <ul className="vorn-search-results">
          <li className="vorn-search-section-header">Not in your library</li>
          {unowned.map((r) => (
            <li key={`${r.mediaType}-${r.tmdbId}`}>
              <span className="vorn-search-unowned-title">
                {r.title}
                <span className="vorn-search-kind">{r.mediaType}</span>
              </span>
              {requestedIds.has(r.tmdbId) ? (
                <span className="vorn-status-badge vorn-status-badge-pending">Requested</span>
              ) : (
                <button type="button" onClick={() => handleRequest(r)} disabled={requestingId === r.tmdbId}>
                  {requestingId === r.tmdbId ? 'Requesting…' : 'Request'}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

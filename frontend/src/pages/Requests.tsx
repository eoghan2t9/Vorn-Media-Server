import { useEffect, useState, type FormEvent } from 'react'
import {
  ApiError,
  createContentRequest,
  deleteContentRequest,
  discoverSearch,
  listMyContentRequests,
  type ContentRequest,
  type DiscoverResult,
} from '../api/client'
import { Poster } from '../components/Poster'
import { Select } from '../components/Select'
import './ViewerHome.css'
import './AdminUsers.css'
import './Requests.css'

export function Requests() {
  const [query, setQuery] = useState('')
  const [mediaType, setMediaType] = useState<'movie' | 'series'>('movie')
  const [results, setResults] = useState<DiscoverResult[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [myRequests, setMyRequests] = useState<ContentRequest[]>([])
  const [requestingId, setRequestingId] = useState<number | null>(null)

  function loadMyRequests() {
    listMyContentRequests()
      .then(setMyRequests)
      .catch(() => {})
  }

  useEffect(loadMyRequests, [])

  async function handleSearch(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSearching(true)
    try {
      setResults(await discoverSearch(query, mediaType))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Search failed')
    } finally {
      setSearching(false)
    }
  }

  async function handleRequest(result: DiscoverResult) {
    setError(null)
    setRequestingId(result.tmdbId)
    try {
      await createContentRequest({
        mediaType,
        tmdbId: result.tmdbId,
        title: result.title,
        overview: result.overview,
        releaseDate: result.releaseDate,
        posterUrl: result.posterUrl,
      })
      loadMyRequests()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to submit request')
    } finally {
      setRequestingId(null)
    }
  }

  async function handleWithdraw(id: string) {
    try {
      await deleteContentRequest(id)
      setMyRequests((list) => list.filter((r) => r.id !== id))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to withdraw request')
    }
  }

  function existingRequestFor(tmdbId: number, type: 'movie' | 'series') {
    return myRequests.find((r) => r.tmdbId === tmdbId && r.mediaType === type)
  }

  return (
    <div className="vorn-requests-page">
      <div className="vorn-admin-page-header">
        <h1>Requests</h1>
        <p className="vorn-admin-page-subtitle">Search for a movie or series and ask for it to be added.</p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      <form className="vorn-inline-form" onSubmit={handleSearch}>
        <Select
          value={mediaType}
          onChange={(v) => setMediaType(v as 'movie' | 'series')}
          options={[
            { value: 'movie', label: 'Movie' },
            { value: 'series', label: 'Series' },
          ]}
        />
        <input
          placeholder="Search for a title…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          style={{ minWidth: '20rem' }}
          required
        />
        <button type="submit" disabled={searching || !query.trim()}>
          {searching ? 'Searching…' : 'Search'}
        </button>
      </form>

      {results &&
        (results.length === 0 ? (
          <p className="vorn-empty">No results found.</p>
        ) : (
          <div className="vorn-card-grid">
            {results.map((r) => {
              const existing = existingRequestFor(r.tmdbId, mediaType)
              return (
                <div className="vorn-card" key={r.tmdbId}>
                  <Poster title={r.title} posterUrl={r.posterUrl} />
                  <div className="vorn-card-title">{r.title}</div>
                  {r.releaseDate && <div className="vorn-card-meta">{r.releaseDate.slice(0, 4)}</div>}
                  {existing ? (
                    <span className={`vorn-status-badge vorn-status-badge-${existing.status}`}>{existing.status}</span>
                  ) : (
                    <button type="button" onClick={() => handleRequest(r)} disabled={requestingId === r.tmdbId}>
                      {requestingId === r.tmdbId ? 'Requesting…' : 'Request'}
                    </button>
                  )}
                </div>
              )
            })}
          </div>
        ))}

      <div className="vorn-panel" style={{ marginTop: '2rem' }}>
        <div className="vorn-panel-header">
          <h2>My requests</h2>
        </div>
        {myRequests.length === 0 ? (
          <p>You haven't requested anything yet.</p>
        ) : (
          <div className="vorn-table-wrap">
            <table className="vorn-table">
              <thead>
                <tr>
                  <th>Title</th>
                  <th>Type</th>
                  <th>Status</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {myRequests.map((r) => (
                  <tr key={r.id}>
                    <td>{r.title}</td>
                    <td>{r.mediaType}</td>
                    <td>
                      <span className={`vorn-status-badge vorn-status-badge-${r.status}`}>{r.status}</span>
                    </td>
                    <td>
                      {r.status === 'pending' && (
                        <button type="button" className="vorn-btn-danger" onClick={() => handleWithdraw(r.id)}>
                          Withdraw
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}

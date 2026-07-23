import { useEffect, useRef, useState, type FormEvent } from 'react'
import {
  ApiError,
  addMagnet,
  addTorrentFile,
  createTorrentIndexer,
  deleteTorrentIndexer,
  listLibraries,
  listTorrentIndexers,
  listTorrents,
  removeTorrent,
  searchTorrents,
  type Library,
  type Torrent,
  type TorrentIndexer,
  type TorrentSearchResult,
} from '../api/client'
import './AdminUsers.css'

function formatBytes(n: number) {
  if (n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  return `${(n / 1024 ** i).toFixed(1)} ${units[i]}`
}

export function AdminTorrents() {
  const [torrents, setTorrents] = useState<Torrent[]>([])
  const [libraries, setLibraries] = useState<Library[]>([])
  const [indexers, setIndexers] = useState<TorrentIndexer[]>([])
  const [error, setError] = useState<string | null>(null)

  const [magnetUri, setMagnetUri] = useState('')
  const [libraryId, setLibraryId] = useState('')
  const [sequential, setSequential] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [indexerName, setIndexerName] = useState('')
  const [indexerBaseUrl, setIndexerBaseUrl] = useState('')
  const [indexerApiKey, setIndexerApiKey] = useState('')

  const [query, setQuery] = useState('')
  const [results, setResults] = useState<TorrentSearchResult[] | null>(null)
  const [searching, setSearching] = useState(false)

  async function refreshTorrents() {
    setTorrents(await listTorrents())
  }

  useEffect(() => {
    Promise.all([refreshTorrents(), listLibraries().then(setLibraries), listTorrentIndexers().then(setIndexers)]).catch(
      (err) => setError(err instanceof ApiError ? err.message : String(err)),
    )
    const interval = setInterval(() => {
      refreshTorrents().catch(() => {})
    }, 2000)
    return () => clearInterval(interval)
  }, [])

  async function handleAddMagnet(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await addMagnet({ magnetUri, libraryId: libraryId || undefined, sequential })
      setMagnetUri('')
      await refreshTorrents()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add magnet')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleFileChange() {
    const file = fileInputRef.current?.files?.[0]
    if (!file) return
    setError(null)
    setSubmitting(true)
    try {
      await addTorrentFile(file, { libraryId: libraryId || undefined, sequential })
      await refreshTorrents()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add torrent file')
    } finally {
      setSubmitting(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  async function handleRemove(id: string, deleteFiles: boolean) {
    try {
      await removeTorrent(id, deleteFiles)
      await refreshTorrents()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to remove torrent')
    }
  }

  async function handleAddIndexer(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const idx = await createTorrentIndexer({ name: indexerName, baseUrl: indexerBaseUrl, apiKey: indexerApiKey })
      setIndexers((list) => [...list, idx])
      setIndexerName('')
      setIndexerBaseUrl('')
      setIndexerApiKey('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add indexer')
    }
  }

  async function handleDeleteIndexer(id: string) {
    try {
      await deleteTorrentIndexer(id)
      setIndexers((list) => list.filter((i) => i.id !== id))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete indexer')
    }
  }

  async function handleSearch(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSearching(true)
    try {
      setResults(await searchTorrents(query))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Search failed')
    } finally {
      setSearching(false)
    }
  }

  async function handleDownloadResult(res: TorrentSearchResult) {
    if (!res.downloadUrl.startsWith('magnet:')) {
      setError('This result is a .torrent file link, not a magnet — open it and upload the file below.')
      return
    }
    setError(null)
    try {
      await addMagnet({ magnetUri: res.downloadUrl, libraryId: libraryId || undefined, sequential })
      await refreshTorrents()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add magnet')
    }
  }

  return (
    <section className="vorn-admin-users">
      <h1>Torrents</h1>
      {error && <p className="vorn-form-error">{error}</p>}

      <div className="vorn-table-wrap">
      <table className="vorn-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Status</th>
            <th>Progress</th>
            <th>Sequential</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {torrents.map((t) => (
            <tr key={t.id}>
              <td>{t.name || t.infoHash}</td>
              <td>{t.status === 'error' ? `error: ${t.error}` : t.status}</td>
              <td>
                {formatBytes(t.bytesDone)} / {formatBytes(t.bytesTotal)}
                {t.bytesTotal > 0 ? ` (${Math.floor((100 * t.bytesDone) / t.bytesTotal)}%)` : ''}
              </td>
              <td>{t.sequential ? 'yes' : 'no'}</td>
              <td>
                <button type="button" onClick={() => handleRemove(t.id, false)}>
                  Remove
                </button>{' '}
                <button type="button" onClick={() => handleRemove(t.id, true)}>
                  Remove + delete files
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>

      <h2>Add torrent</h2>
      <form className="vorn-inline-form" onSubmit={handleAddMagnet}>
        <input
          placeholder="Magnet URI"
          value={magnetUri}
          onChange={(e) => setMagnetUri(e.target.value)}
          style={{ minWidth: '20rem' }}
          required
        />
        <select value={libraryId} onChange={(e) => setLibraryId(e.target.value)}>
          <option value="">No destination library (won't auto-add)</option>
          {libraries.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </select>
        <label>
          <input type="checkbox" checked={sequential} onChange={(e) => setSequential(e.target.checked)} /> Sequential
        </label>
        <button type="submit" disabled={submitting}>
          {submitting ? 'Adding…' : 'Add magnet'}
        </button>
      </form>
      <p>
        Or upload a .torrent file: <input ref={fileInputRef} type="file" accept=".torrent" onChange={handleFileChange} />
      </p>

      <h2>Search indexers</h2>
      <form className="vorn-inline-form" onSubmit={handleSearch}>
        <input placeholder="Search query" value={query} onChange={(e) => setQuery(e.target.value)} required />
        <button type="submit" disabled={searching}>
          {searching ? 'Searching…' : 'Search'}
        </button>
      </form>
      {results && (
        <div className="vorn-table-wrap">
        <table className="vorn-table">
          <thead>
            <tr>
              <th>Title</th>
              <th>Indexer</th>
              <th>Size</th>
              <th>Seeders</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {results.map((r, i) => (
              <tr key={i}>
                <td>{r.title}</td>
                <td>{r.indexerName}</td>
                <td>{formatBytes(r.sizeBytes)}</td>
                <td>{r.seeders}</td>
                <td>
                  <button type="button" onClick={() => handleDownloadResult(r)}>
                    Download
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
      )}

      <h2>Indexers</h2>
      <div className="vorn-table-wrap">
      <table className="vorn-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Base URL</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {indexers.map((idx) => (
            <tr key={idx.id}>
              <td>{idx.name}</td>
              <td>{idx.baseUrl}</td>
              <td>
                <button type="button" onClick={() => handleDeleteIndexer(idx.id)}>
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>
      <form className="vorn-inline-form" onSubmit={handleAddIndexer}>
        <input placeholder="Name" value={indexerName} onChange={(e) => setIndexerName(e.target.value)} required />
        <input
          placeholder="Torznab base URL"
          value={indexerBaseUrl}
          onChange={(e) => setIndexerBaseUrl(e.target.value)}
          style={{ minWidth: '16rem' }}
          required
        />
        <input placeholder="API key (optional)" value={indexerApiKey} onChange={(e) => setIndexerApiKey(e.target.value)} />
        <button type="submit">Add indexer</button>
      </form>
    </section>
  )
}

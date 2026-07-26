import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
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
  testTorrentIndexer,
  updateTorrentIndexer,
  type Library,
  type Torrent,
  type TorrentIndexer,
  type TorrentIndexerProvider,
  type TorrentSearchResult,
} from '../api/client'
import { FileDropzone, type FileDropzoneHandle } from '../components/FileDropzone'
import { Pagination } from '../components/Pagination'
import { Select } from '../components/Select'
import { usePagination } from '../components/usePagination'
import './AdminUsers.css'

function formatBytes(n: number) {
  if (n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  return `${(n / 1024 ** i).toFixed(1)} ${units[i]}`
}

// Torznab is the protocol indexer-manager apps expose, not something public
// trackers speak directly -- these presets fill in the fixed structural
// part of each app's per-indexer Torznab URL (which SearchIndexer/TestIndexer
// complete by appending "/api"), leaving the <...> placeholder for the admin
// to swap for their own indexer's id/slug from that app's UI.
const INDEXER_PRESETS: { label: string; name: string; baseUrl: string }[] = [
  {
    label: 'Prowlarr',
    name: 'Prowlarr',
    baseUrl: 'http://localhost:9696/<indexer-id>',
  },
]

export function AdminTorrents() {
  const [searchParams] = useSearchParams()
  const [torrents, setTorrents] = useState<Torrent[]>([])
  const [libraries, setLibraries] = useState<Library[]>([])
  const [indexers, setIndexers] = useState<TorrentIndexer[]>([])
  const [error, setError] = useState<string | null>(null)

  const [magnetUri, setMagnetUri] = useState('')
  const [libraryId, setLibraryId] = useState('')
  const [sequential, setSequential] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const dropzoneRef = useRef<FileDropzoneHandle>(null)

  const [indexerProvider, setIndexerProvider] = useState<TorrentIndexerProvider>('torznab')
  const [indexerPreset, setIndexerPreset] = useState('')
  const [indexerName, setIndexerName] = useState('')
  const [indexerBaseUrl, setIndexerBaseUrl] = useState('')
  const [indexerApiKey, setIndexerApiKey] = useState('')
  const [testingIndexer, setTestingIndexer] = useState(false)
  const [indexerTestResult, setIndexerTestResult] = useState<{ ok: boolean; message: string } | null>(null)
  // Set while editing an existing indexer (row's Edit button) instead of
  // adding a new one -- reuses the same form, just routed to the update API
  // on submit. The API key field stays blank in this mode (the list
  // endpoint never echoes it back), meaning "keep the current key".
  const [editingIndexerId, setEditingIndexerId] = useState<string | null>(null)

  // Prefilled from ?q=... when arriving via the "Search torrents" deep link
  // on Admin > Requests -- the query still auto-runs below rather than just
  // sitting in the box, since the whole point of that link is skipping the
  // retype-and-hit-search step.
  const [query, setQuery] = useState(() => searchParams.get('q') ?? '')
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

  async function handleFileSelected(file: File) {
    setError(null)
    setSubmitting(true)
    try {
      await addTorrentFile(file, { libraryId: libraryId || undefined, sequential })
      await refreshTorrents()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add torrent file')
    } finally {
      setSubmitting(false)
      dropzoneRef.current?.reset()
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

  function handleIndexerProvider(provider: TorrentIndexerProvider) {
    setIndexerProvider(provider)
    setIndexerPreset('')
    setIndexerTestResult(null)
    if (provider === 'torbox') {
      setIndexerName((name) => name || 'TorBox')
    }
  }

  function handleIndexerPreset(id: string) {
    setIndexerPreset(id)
    const preset = INDEXER_PRESETS.find((p) => p.label === id)
    if (preset) {
      setIndexerName(preset.name)
      setIndexerBaseUrl(preset.baseUrl)
      setIndexerTestResult(null)
    }
  }

  function resetIndexerForm() {
    setEditingIndexerId(null)
    setIndexerProvider('torznab')
    setIndexerPreset('')
    setIndexerName('')
    setIndexerBaseUrl('')
    setIndexerApiKey('')
    setIndexerTestResult(null)
  }

  function handleEditIndexer(idx: TorrentIndexer) {
    setError(null)
    setEditingIndexerId(idx.id)
    setIndexerProvider(idx.provider)
    setIndexerPreset('')
    setIndexerName(idx.name)
    setIndexerBaseUrl(idx.provider === 'torbox' ? '' : idx.baseUrl)
    setIndexerApiKey('')
    setIndexerTestResult(null)
  }

  async function handleAddIndexer(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      if (editingIndexerId) {
        const idx = await updateTorrentIndexer(editingIndexerId, {
          name: indexerName,
          baseUrl: indexerProvider === 'torbox' ? '' : indexerBaseUrl,
          apiKey: indexerApiKey || undefined,
          provider: indexerProvider,
        })
        setIndexers((list) => list.map((i) => (i.id === idx.id ? idx : i)))
      } else {
        const idx = await createTorrentIndexer({
          name: indexerName,
          baseUrl: indexerProvider === 'torbox' ? '' : indexerBaseUrl,
          apiKey: indexerApiKey,
          provider: indexerProvider,
        })
        setIndexers((list) => [...list, idx])
      }
      resetIndexerForm()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : `Failed to ${editingIndexerId ? 'save' : 'add'} indexer`)
    }
  }

  async function handleTestIndexer() {
    setIndexerTestResult(null)
    setTestingIndexer(true)
    try {
      const result = await testTorrentIndexer(
        indexerProvider === 'torbox'
          ? { provider: 'torbox', apiKey: indexerApiKey }
          : { baseUrl: indexerBaseUrl, apiKey: indexerApiKey || undefined },
      )
      setIndexerTestResult(
        result.ok ? { ok: true, message: 'Indexer responded successfully.' } : { ok: false, message: result.error ?? 'Test failed.' },
      )
    } catch (err) {
      setIndexerTestResult({ ok: false, message: err instanceof ApiError ? err.message : 'Failed to test indexer' })
    } finally {
      setTestingIndexer(false)
    }
  }

  async function handleDeleteIndexer(id: string) {
    try {
      await deleteTorrentIndexer(id)
      setIndexers((list) => list.filter((i) => i.id !== id))
      if (editingIndexerId === id) resetIndexerForm()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete indexer')
    }
  }

  async function runSearch(q: string) {
    setError(null)
    setSearching(true)
    try {
      setResults(await searchTorrents(q))
      resultsPage.setPage(1)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Search failed')
    } finally {
      setSearching(false)
    }
  }

  async function handleSearch(e: FormEvent) {
    e.preventDefault()
    await runSearch(query)
  }

  useEffect(() => {
    // Only for the deep-link case ("Search torrents" on Admin > Requests)
    // -- searchParams never changes again after mount since nothing on
    // this page calls setSearchParams, so this only ever runs once.
    const q = searchParams.get('q')
    if (q) runSearch(q)
  }, [searchParams])

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

  const torrentsPage = usePagination(torrents)
  const resultsPage = usePagination(results ?? [])

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Torrents</h1>
        <p className="vorn-admin-page-subtitle">Add magnets/files directly, or search configured Torznab indexers.</p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Active torrents</h2>
        </div>
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
            {torrentsPage.pageItems.map((t) => (
              <tr key={t.id}>
                <td data-label="Name">{t.name || t.infoHash}</td>
                <td data-label="Status">{t.status === 'error' ? `error: ${t.error}` : t.status}</td>
                <td data-label="Progress">
                  {formatBytes(t.bytesDone)} / {formatBytes(t.bytesTotal)}
                  {t.bytesTotal > 0 ? ` (${Math.floor((100 * t.bytesDone) / t.bytesTotal)}%)` : ''}
                </td>
                <td data-label="Sequential">{t.sequential ? 'yes' : 'no'}</td>
                <td data-label="Actions">
                  <div className="vorn-button-group">
                    <button type="button" className="vorn-btn-danger" onClick={() => handleRemove(t.id, false)}>
                      Remove
                    </button>
                    <button type="button" className="vorn-btn-danger" onClick={() => handleRemove(t.id, true)}>
                      Remove + delete files
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
        <Pagination page={torrentsPage.page} totalPages={torrentsPage.totalPages} onChange={torrentsPage.setPage} />
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Add torrent</h2>
        </div>
        <form className="vorn-inline-form" onSubmit={handleAddMagnet}>
          <input
            placeholder="Magnet URI"
            value={magnetUri}
            onChange={(e) => setMagnetUri(e.target.value)}
            style={{ minWidth: '20rem' }}
            required
          />
          <Select
            value={libraryId}
            onChange={setLibraryId}
            placeholder="No destination library"
            options={libraries.map((l) => ({ value: l.id, label: l.name }))}
          />
          <label>
            <input type="checkbox" checked={sequential} onChange={(e) => setSequential(e.target.checked)} /> Sequential
          </label>
          <button type="submit" disabled={submitting}>
            {submitting ? 'Adding…' : 'Add magnet'}
          </button>
        </form>
        <p className="vorn-panel-subtitle" style={{ margin: '1rem 0 0.5rem' }}>
          Or upload a .torrent file:
        </p>
        <FileDropzone
          ref={dropzoneRef}
          accept=".torrent"
          hint=".torrent files"
          disabled={submitting}
          onFile={handleFileSelected}
        />
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Search indexers</h2>
        </div>
        <form className="vorn-inline-form" onSubmit={handleSearch}>
          <input placeholder="Search query" value={query} onChange={(e) => setQuery(e.target.value)} required />
          <button type="submit" disabled={searching}>
            {searching ? 'Searching…' : 'Search'}
          </button>
        </form>
        {results && (
          <>
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
              {resultsPage.pageItems.map((r, i) => (
                <tr key={i}>
                  <td data-label="Title">{r.title}</td>
                  <td data-label="Indexer">{r.indexerName}</td>
                  <td data-label="Size">{formatBytes(r.sizeBytes)}</td>
                  <td data-label="Seeders">{r.seeders}</td>
                  <td data-label="Actions">
                    <button type="button" onClick={() => handleDownloadResult(r)}>
                      Download
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
          <Pagination page={resultsPage.page} totalPages={resultsPage.totalPages} onChange={resultsPage.setPage} />
          </>
        )}
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Indexers</h2>
        </div>
        <div className="vorn-table-wrap">
        <table className="vorn-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Type</th>
              <th>Base URL</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {indexers.map((idx) => (
              <tr key={idx.id}>
                <td data-label="Name">{idx.name}</td>
                <td data-label="Type">{idx.provider === 'torbox' ? 'TorBox' : 'Torznab'}</td>
                <td data-label="Base URL">{idx.provider === 'torbox' ? '—' : idx.baseUrl}</td>
                <td data-label="Actions">
                  <div className="vorn-button-group">
                    <button type="button" onClick={() => handleEditIndexer(idx)}>
                      Edit
                    </button>
                    <button type="button" className="vorn-btn-danger" onClick={() => handleDeleteIndexer(idx.id)}>
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
        <form className="vorn-inline-form" onSubmit={handleAddIndexer} style={{ marginTop: '1rem' }}>
          <Select
            value={indexerProvider}
            onChange={(v) => handleIndexerProvider(v as TorrentIndexerProvider)}
            options={[
              { value: 'torznab', label: 'Torznab (Prowlarr, Jackett, ...)' },
              { value: 'torbox', label: 'TorBox' },
            ]}
          />
          <input placeholder="Name" value={indexerName} onChange={(e) => setIndexerName(e.target.value)} required />
          {indexerProvider === 'torbox' ? (
            <input
              placeholder={editingIndexerId ? 'TorBox API key (leave blank to keep current)' : 'TorBox API key'}
              type="password"
              value={indexerApiKey}
              onChange={(e) => setIndexerApiKey(e.target.value)}
              style={{ minWidth: '16rem' }}
              required={!editingIndexerId}
            />
          ) : (
            <>
              <Select
                value={indexerPreset}
                onChange={handleIndexerPreset}
                placeholder="Preset (optional)"
                options={INDEXER_PRESETS.map((p) => ({ value: p.label, label: p.label }))}
              />
              <input
                placeholder="Torznab base URL"
                value={indexerBaseUrl}
                onChange={(e) => setIndexerBaseUrl(e.target.value)}
                style={{ minWidth: '16rem' }}
                required
              />
              <input
                placeholder={editingIndexerId ? 'API key (leave blank to keep current)' : 'API key (optional)'}
                value={indexerApiKey}
                onChange={(e) => setIndexerApiKey(e.target.value)}
              />
            </>
          )}
          <button
            type="button"
            onClick={handleTestIndexer}
            disabled={testingIndexer || (indexerProvider === 'torbox' ? !indexerApiKey : !indexerBaseUrl)}
          >
            {testingIndexer ? 'Testing…' : 'Test'}
          </button>
          <button type="submit">{editingIndexerId ? 'Save indexer' : 'Add indexer'}</button>
          {editingIndexerId && (
            <button type="button" onClick={resetIndexerForm}>
              Cancel
            </button>
          )}
        </form>
        {indexerProvider === 'torbox' && (
          <p className="vorn-panel-subtitle" style={{ margin: '0.6rem 0 0' }}>
            TorBox's torrent search only works for a movie/episode Vorn already knows the IMDb ID for -- results appear
            alongside any Torznab indexers once that's available.
          </p>
        )}
        {indexerPreset && (
          <p className="vorn-panel-subtitle" style={{ margin: '0.6rem 0 0' }}>
            Replace <code>&lt;indexer-id&gt;</code> in the base URL with the id/slug shown for this indexer in{' '}
            {indexerPreset}'s own UI.
          </p>
        )}
        {indexerTestResult && (
          <p className={indexerTestResult.ok ? 'vorn-test-result-ok' : 'vorn-form-error'} style={{ marginTop: '0.6rem' }}>
            {indexerTestResult.message}
          </p>
        )}
      </div>
    </section>
  )
}

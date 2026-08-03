import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  ApiError,
  addDebridLink,
  createTorrentIndexer,
  deleteTorrentIndexer,
  listDebridAccounts,
  listLibraries,
  listTorrentIndexers,
  magnetFromDownloadUrl,
  magnetFromTorrentFile,
  searchTorrents,
  testTorrentIndexer,
  updateTorrentIndexer,
  type DebridAccount,
  type Library,
  type TorrentIndexer,
  type TorrentIndexerProvider,
  type TorrentSearchResult,
} from '../api/client'
import { FileDropzone, type FileDropzoneHandle } from '../components/FileDropzone'
import { Pagination } from '../components/Pagination'
import { Select } from '../components/Select'
import { usePagination } from '../components/usePagination'
import './AdminHome.css'
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
  const [libraries, setLibraries] = useState<Library[]>([])
  const [indexers, setIndexers] = useState<TorrentIndexer[]>([])
  const [accounts, setAccounts] = useState<DebridAccount[]>([])
  const [error, setError] = useState<string | null>(null)

  const [accountId, setAccountId] = useState('')
  const [magnetUri, setMagnetUri] = useState('')
  const [libraryId, setLibraryId] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const dropzoneRef = useRef<FileDropzoneHandle>(null)

  const [indexerProvider, setIndexerProvider] = useState<TorrentIndexerProvider>('torznab')
  const [indexerPreset, setIndexerPreset] = useState('')
  const [indexerName, setIndexerName] = useState('')
  const [indexerBaseUrl, setIndexerBaseUrl] = useState('')
  const [indexerApiKey, setIndexerApiKey] = useState('')
  const [testingIndexer, setTestingIndexer] = useState(false)
  const [indexerTestResult, setIndexerTestResult] = useState<{
    ok: boolean
    message: string
    supportsImdbSearch?: boolean
    supportsTvdbSearch?: boolean
  } | null>(null)
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

  useEffect(() => {
    Promise.all([
      listLibraries().then(setLibraries),
      listTorrentIndexers().then(setIndexers),
      listDebridAccounts().then(setAccounts),
    ]).catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  // resolveViaDebrid is the one place a magnet actually gets acted on --
  // every entry point (pasted magnet, uploaded .torrent file, a search
  // result) funnels into this, which just calls the same debrid resolve
  // AdminDebrid.tsx's own "Add magnet/hash" form uses. Vorn never
  // downloads the torrent itself; the debrid provider fetches from the
  // swarm on its own infrastructure and hands back a direct stream URL.
  async function resolveViaDebrid(sourceRef: string, name?: string) {
    await addDebridLink({ accountId, sourceRef, name, libraryId: libraryId || undefined })
  }

  async function handleResolveMagnet(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await resolveViaDebrid(magnetUri)
      setMagnetUri('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to resolve magnet')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleFileSelected(file: File) {
    setError(null)
    setSubmitting(true)
    try {
      const { magnetUri: extracted } = await magnetFromTorrentFile(file)
      await resolveViaDebrid(extracted, file.name)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to resolve torrent file')
    } finally {
      setSubmitting(false)
      dropzoneRef.current?.reset()
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
    // Seed from this indexer's already-known capability rather than forcing
    // a re-test on every edit -- changing baseUrl/apiKey (the only fields
    // that could actually change what it supports) resets this back to
    // null via their onChange handlers, so Save re-requires a fresh Test
    // exactly when the old result might no longer be valid.
    setIndexerTestResult(
      idx.provider === 'torbox' || (!idx.supportsImdbSearch && !idx.supportsTvdbSearch)
        ? null
        : { ok: true, message: 'Using previously confirmed capability.', supportsImdbSearch: idx.supportsImdbSearch, supportsTvdbSearch: idx.supportsTvdbSearch },
    )
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
      if (!result.ok) {
        setIndexerTestResult({ ok: false, message: result.error ?? 'Test failed.' })
      } else if (indexerProvider !== 'torbox' && !result.supportsImdbSearch && !result.supportsTvdbSearch) {
        setIndexerTestResult({
          ok: false,
          message: 'This indexer supports neither IMDb nor TVDB id search, which Vorn requires -- it cannot be used.',
        })
      } else {
        setIndexerTestResult({
          ok: true,
          message: 'Indexer responded successfully.',
          supportsImdbSearch: result.supportsImdbSearch,
          supportsTvdbSearch: result.supportsTvdbSearch,
        })
      }
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

  async function handleResolveResult(res: TorrentSearchResult) {
    setError(null)
    setSubmitting(true)
    try {
      if (res.downloadUrl.startsWith('magnet:')) {
        await resolveViaDebrid(res.downloadUrl, res.title)
      } else {
        const { magnetUri: extracted } = await magnetFromDownloadUrl(res.downloadUrl)
        await resolveViaDebrid(extracted, res.title)
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to resolve')
    } finally {
      setSubmitting(false)
    }
  }

  const resultsPage = usePagination(results ?? [])

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Torrents</h1>
        <p className="vorn-admin-page-subtitle">
          Search configured Torznab indexers, or resolve a magnet/.torrent file through a debrid provider -- nothing is
          ever downloaded to this server.
        </p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Resolve via debrid</h2>
        </div>
        {accounts.length === 0 && (
          <p className="vorn-panel-subtitle">
            No debrid accounts configured yet -- add one on the{' '}
            <Link to="/admin/debrid">Debrid</Link> page first.
          </p>
        )}
        <form className="vorn-inline-form" onSubmit={handleResolveMagnet}>
          <Select
            value={accountId}
            onChange={setAccountId}
            placeholder="Select account…"
            options={accounts.map((a) => ({
              value: a.id,
              label: `${a.provider}${!a.enabled ? ' (disabled)' : ''}`,
            }))}
          />
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
          <button type="submit" disabled={submitting || !accountId}>
            {submitting ? 'Resolving…' : 'Resolve'}
          </button>
        </form>
        <p className="vorn-panel-subtitle" style={{ margin: '1rem 0 0.5rem' }}>
          Or upload a .torrent file (its magnet is extracted locally, nothing is downloaded):
        </p>
        <FileDropzone
          ref={dropzoneRef}
          accept=".torrent"
          hint=".torrent files"
          disabled={submitting || !accountId}
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
                  <td>{r.title}</td>
                  <td>{r.indexerName}</td>
                  <td>{formatBytes(r.sizeBytes)}</td>
                  <td>{r.seeders}</td>
                  <td>
                    <button type="button" onClick={() => handleResolveResult(r)} disabled={submitting || !accountId}>
                      Resolve
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
              <th>ID search</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {indexers.map((idx) => (
              <tr key={idx.id}>
                <td>{idx.name}</td>
                <td>{idx.provider === 'torbox' ? 'TorBox' : 'Torznab'}</td>
                <td>{idx.provider === 'torbox' ? '—' : idx.baseUrl}</td>
                <td>
                  {[idx.supportsImdbSearch && 'IMDb', idx.supportsTvdbSearch && 'TVDB'].filter(Boolean).join(', ') || 'none'}
                </td>
                <td>{idx.enabled ? 'Enabled' : `Disabled${idx.disabledReason ? `: ${idx.disabledReason}` : ''}`}</td>
                <td>
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
                onChange={(e) => {
                  setIndexerBaseUrl(e.target.value)
                  setIndexerTestResult(null)
                }}
                style={{ minWidth: '16rem' }}
                required
              />
              <input
                placeholder={editingIndexerId ? 'API key (leave blank to keep current)' : 'API key (optional)'}
                value={indexerApiKey}
                onChange={(e) => {
                  setIndexerApiKey(e.target.value)
                  setIndexerTestResult(null)
                }}
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
          <button
            type="submit"
            disabled={indexerProvider !== 'torbox' && !(indexerTestResult?.ok && (indexerTestResult.supportsImdbSearch || indexerTestResult.supportsTvdbSearch))}
          >
            {editingIndexerId ? 'Save indexer' : 'Add indexer'}
          </button>
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
        {indexerProvider !== 'torbox' &&
          !(indexerTestResult?.ok && (indexerTestResult.supportsImdbSearch || indexerTestResult.supportsTvdbSearch)) && (
            <p className="vorn-panel-subtitle" style={{ margin: '0.6rem 0 0' }}>
              Vorn only searches indexers by IMDb/TVDB id -- run Test and confirm it supports one of them before saving.
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

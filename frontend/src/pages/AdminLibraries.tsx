import { Fragment, useEffect, useState, type FormEvent } from 'react'
import {
  ApiError,
  createLibrary,
  createWebDAVFolder,
  deleteLibrary,
  deleteWebDAVFolder,
  getMetadataJob,
  getQualityProfile,
  getScanJob,
  listLibraries,
  listWebDAVFolders,
  reorderLibraries,
  startLibraryScan,
  startMetadataSync,
  updateLibrary,
  updateQualityProfile,
  updateWebDAVFolder,
  type Library,
  type LibraryType,
  type MetadataJob,
  type QualityProfile,
  type ScanJob,
  type WebDAVFolder,
} from '../api/client'
import { DirectoryBrowser } from '../components/DirectoryBrowser'
import { Select } from '../components/Select'
import { FolderIcon } from '../components/icons'
import './AdminUsers.css'

export function AdminLibraries() {
  const [libraries, setLibraries] = useState<Library[]>([])
  const [scanJobs, setScanJobs] = useState<Record<string, ScanJob>>({})
  const [metadataJobs, setMetadataJobs] = useState<Record<string, MetadataJob>>({})
  const [error, setError] = useState<string | null>(null)

  const [name, setName] = useState('')
  const [type, setType] = useState<LibraryType>('movie')
  const [is4K, setIs4K] = useState(false)
  const [folder, setFolder] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [browserOpen, setBrowserOpen] = useState(false)

  const [qualityOpenFor, setQualityOpenFor] = useState<string | null>(null)
  const [qualityProfile, setQualityProfile] = useState<QualityProfile | null>(null)
  const [qualityLoading, setQualityLoading] = useState(false)
  const [qualitySaving, setQualitySaving] = useState(false)

  // WebDAV state
  const [webdavFolders, setWebdavFolders] = useState<Record<string, WebDAVFolder[]>>({})
  const [webdavOpenFor, setWebdavOpenFor] = useState<string | null>(null)
  const [webdavUrl, setWebdavUrl] = useState('')
  const [webdavApiKey, setWebdavApiKey] = useState('')
  const [webdavSubmitting, setWebdavSubmitting] = useState(false)

  async function refresh() {
    setLibraries(await listLibraries())
  }

  useEffect(() => {
    refresh().catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  async function loadWebdavFolders(libraryId: string) {
    try {
      const folders = await listWebDAVFolders(libraryId)
      setWebdavFolders((prev) => ({ ...prev, [libraryId]: folders }))
    } catch {
      // silently ignore -- webdav folders are optional
    }
  }

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await createLibrary({ name, type, is4K, folders: folder ? [folder] : [] })
      setName('')
      setFolder('')
      setIs4K(false)
      await refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to create library')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleToggleDefaultTarget(l: Library) {
    try {
      await updateLibrary(l.id, { defaultRequestTarget: !l.defaultRequestTarget })
      await refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to update default request target')
    }
  }

  async function handleMove(index: number, direction: -1 | 1) {
    const target = index + direction
    if (target < 0 || target >= libraries.length) return
    const reordered = [...libraries]
    ;[reordered[index], reordered[target]] = [reordered[target], reordered[index]]
    setLibraries(reordered)
    try {
      await reorderLibraries(reordered.map((l) => l.id))
    } catch (err) {
      setLibraries(libraries)
      setError(err instanceof ApiError ? err.message : 'Failed to reorder libraries')
    }
  }

  async function handleDelete(id: string) {
    try {
      await deleteLibrary(id)
      await refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete library')
    }
  }

  async function handleScan(id: string) {
    try {
      const job = await startLibraryScan(id)
      setScanJobs((j) => ({ ...j, [id]: job }))
      const interval = setInterval(async () => {
        const updated = await getScanJob(job.id)
        setScanJobs((j) => ({ ...j, [id]: updated }))
        if (updated.status !== 'running') clearInterval(interval)
      }, 500)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to start scan')
    }
  }

  async function handleSyncMetadata(id: string) {
    try {
      const job = await startMetadataSync(id)
      setMetadataJobs((j) => ({ ...j, [id]: job }))
      const interval = setInterval(async () => {
        const updated = await getMetadataJob(job.id)
        setMetadataJobs((j) => ({ ...j, [id]: updated }))
        if (updated.status !== 'running') clearInterval(interval)
      }, 500)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to start metadata sync (is VORN_TMDB_API_KEY set?)')
    }
  }

  async function handleToggleQuality(id: string) {
    if (qualityOpenFor === id) {
      setQualityOpenFor(null)
      return
    }
    setQualityOpenFor(id)
    setQualityProfile(null)
    setQualityLoading(true)
    try {
      setQualityProfile(await getQualityProfile(id))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to load quality profile')
    } finally {
      setQualityLoading(false)
    }
  }

  async function handleSaveQuality(e: FormEvent) {
    e.preventDefault()
    if (!qualityOpenFor || !qualityProfile) return
    setQualitySaving(true)
    try {
      const updated = await updateQualityProfile(qualityOpenFor, {
        minResolution: qualityProfile.minResolution,
        maxResolution: qualityProfile.maxResolution,
        preferredCodec: qualityProfile.preferredCodec,
        minSeeders: qualityProfile.minSeeders,
        preferRemux: qualityProfile.preferRemux,
      })
      setQualityProfile(updated)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save quality profile')
    } finally {
      setQualitySaving(false)
    }
  }

  async function handleToggleWebdav(libraryId: string) {
    if (webdavOpenFor === libraryId) {
      setWebdavOpenFor(null)
      return
    }
    setWebdavOpenFor(libraryId)
    setWebdavUrl('')
    setWebdavApiKey('')
    await loadWebdavFolders(libraryId)
  }

  async function handleAddWebdav(e: FormEvent) {
    e.preventDefault()
    if (!webdavOpenFor) return
    setWebdavSubmitting(true)
    setError(null)
    try {
      await createWebDAVFolder(webdavOpenFor, {
        url: webdavUrl || undefined,
        apiKey: webdavApiKey,
      })
      setWebdavApiKey('')
      await loadWebdavFolders(webdavOpenFor)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add WebDAV folder')
    } finally {
      setWebdavSubmitting(false)
    }
  }

  async function handleToggleWebdavEnabled(libraryId: string, wf: WebDAVFolder) {
    try {
      await updateWebDAVFolder(libraryId, wf.id, { enabled: !wf.enabled })
      await loadWebdavFolders(libraryId)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to update WebDAV folder')
    }
  }

  async function handleDeleteWebdav(libraryId: string, wf: WebDAVFolder) {
    if (!confirm(`Remove WebDAV folder ${wf.url}? Files already scanned from it will stay in the library.`)) return
    try {
      await deleteWebDAVFolder(libraryId, wf.id)
      await loadWebdavFolders(libraryId)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete WebDAV folder')
    }
  }

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Libraries</h1>
        <p className="vorn-admin-page-subtitle">Scan folders on the server and match them to metadata.</p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>All libraries</h2>
        </div>
        <div className="vorn-table-wrap">
        <table className="vorn-table">
        <thead>
          <tr>
            <th>Order</th>
            <th>Name</th>
            <th>Type</th>
            <th>Folders</th>
            <th>Scan status</th>
            <th>Metadata status</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {libraries.map((l, index) => {
            const scanJob = scanJobs[l.id]
            const metaJob = metadataJobs[l.id]
            const wfs = webdavFolders[l.id] ?? []
            return (
              <Fragment key={l.id}>
                <tr>
                  <td>
                    <div className="vorn-button-group">
                      <button
                        type="button"
                        onClick={() => handleMove(index, -1)}
                        disabled={index === 0}
                        aria-label={`Move ${l.name} up`}
                        title="Move up"
                      >
                        ↑
                      </button>
                      <button
                        type="button"
                        onClick={() => handleMove(index, 1)}
                        disabled={index === libraries.length - 1}
                        aria-label={`Move ${l.name} down`}
                        title="Move down"
                      >
                        ↓
                      </button>
                    </div>
                  </td>
                  <td>
                    {l.name}
                    {l.is4K && (
                      <span className="vorn-user-badge" style={{ marginLeft: '0.5rem' }} title="4K-only library">
                        4K
                      </span>
                    )}
                    {l.defaultRequestTarget && (
                      <span
                        className="vorn-user-badge"
                        style={{ marginLeft: '0.5rem' }}
                        title="Content requests auto-fulfill into this library"
                      >
                        Default target
                      </span>
                    )}
                  </td>
                  <td>{l.type}</td>
                  <td>
                    {l.folders.join(', ')}
                    {wfs.filter((wf) => wf.enabled).map((wf) => (
                      <span
                        key={wf.id}
                        className="vorn-user-badge"
                        style={{ marginLeft: '0.35rem' }}
                        title={`WebDAV: ${wf.url}`}
                      >
                        WebDAV
                      </span>
                    ))}
                  </td>
                  <td>
                    {scanJob ? `${scanJob.status} (${scanJob.filesSynced}/${scanJob.filesFound} files)` : '—'}
                  </td>
                  <td>
                    {metaJob
                      ? metaJob.status === 'failed'
                        ? `failed: ${metaJob.error}`
                        : `${metaJob.status} (${metaJob.itemsMatched}/${metaJob.itemsFound} matched)`
                      : '—'}
                  </td>
                  <td>
                    <div className="vorn-button-group">
                      <button type="button" onClick={() => handleScan(l.id)} disabled={scanJob?.status === 'running'}>
                        {scanJob?.status === 'running' ? 'Scanning…' : 'Scan'}
                      </button>
                      <button
                        type="button"
                        onClick={() => handleSyncMetadata(l.id)}
                        disabled={metaJob?.status === 'running'}
                      >
                        {metaJob?.status === 'running' ? 'Syncing…' : 'Sync metadata'}
                      </button>
                      {(l.type === 'movie' || l.type === 'series') && (
                        <button type="button" onClick={() => handleToggleQuality(l.id)}>
                          {qualityOpenFor === l.id ? 'Hide quality profile' : 'Quality profile'}
                        </button>
                      )}
                      {(l.type === 'movie' || l.type === 'series') && (
                        <button type="button" onClick={() => handleToggleWebdav(l.id)}>
                          {webdavOpenFor === l.id ? 'Hide WebDAV' : 'WebDAV'}
                        </button>
                      )}
                      {(l.type === 'movie' || l.type === 'series') && (
                        <button
                          type="button"
                          onClick={() => handleToggleDefaultTarget(l)}
                          title="Content requests for this media type and quality auto-fulfill into whichever library is the default target"
                        >
                          {l.defaultRequestTarget ? 'Unset default target' : 'Set as default target'}
                        </button>
                      )}
                      <button type="button" className="vorn-btn-danger" onClick={() => handleDelete(l.id)}>
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
                {qualityOpenFor === l.id && (
                  <tr>
                    <td colSpan={7}>
                      {qualityLoading || !qualityProfile ? (
                        <p className="vorn-panel-subtitle">Loading…</p>
                      ) : (
                        <form className="vorn-inline-form" onSubmit={handleSaveQuality}>
                          <label>
                            Min resolution{' '}
                            <select
                              value={qualityProfile.minResolution}
                              onChange={(e) =>
                                setQualityProfile({ ...qualityProfile, minResolution: e.target.value as QualityProfile['minResolution'] })
                              }
                            >
                              <option value="480p">480p</option>
                              <option value="720p">720p</option>
                              <option value="1080p">1080p</option>
                              <option value="2160p">2160p</option>
                            </select>
                          </label>
                          <label>
                            Max resolution{' '}
                            <select
                              value={qualityProfile.maxResolution}
                              onChange={(e) =>
                                setQualityProfile({ ...qualityProfile, maxResolution: e.target.value as QualityProfile['maxResolution'] })
                              }
                            >
                              <option value="480p">480p</option>
                              <option value="720p">720p</option>
                              <option value="1080p">1080p</option>
                              <option value="2160p">2160p</option>
                            </select>
                          </label>
                          <label>
                            Preferred codec{' '}
                            <select
                              value={qualityProfile.preferredCodec}
                              onChange={(e) =>
                                setQualityProfile({ ...qualityProfile, preferredCodec: e.target.value as QualityProfile['preferredCodec'] })
                              }
                            >
                              <option value="">No preference</option>
                              <option value="x264">x264</option>
                              <option value="x265">x265 (HEVC)</option>
                              <option value="av1">AV1</option>
                            </select>
                          </label>
                          <label>
                            Min seeders{' '}
                            <input
                              type="number"
                              min={0}
                              value={qualityProfile.minSeeders}
                              onChange={(e) => setQualityProfile({ ...qualityProfile, minSeeders: Number(e.target.value) })}
                              style={{ width: '5rem' }}
                            />
                          </label>
                          <label>
                            <input
                              type="checkbox"
                              checked={qualityProfile.preferRemux}
                              onChange={(e) => setQualityProfile({ ...qualityProfile, preferRemux: e.target.checked })}
                            />{' '}
                            Prefer remux
                          </label>
                          <button type="submit" disabled={qualitySaving}>
                            {qualitySaving ? 'Saving…' : 'Save'}
                          </button>
                        </form>
                      )}
                    </td>
                  </tr>
                )}
                {webdavOpenFor === l.id && (
                  <tr>
                    <td colSpan={7}>
                      <div style={{ padding: '0.75rem 0' }}>
                        <p className="vorn-panel-subtitle" style={{ margin: '0 0 0.75rem' }}>
                          WebDAV folders for this library — files discovered here are promoted alongside
                          locally-scanned files. Uses your TorBox API key for Basic auth.
                        </p>
                        {wfs.length > 0 && (
                          <div className="vorn-table-wrap" style={{ marginBottom: '1rem' }}>
                            <table className="vorn-table">
                              <thead>
                                <tr>
                                  <th>URL</th>
                                  <th>API Key</th>
                                  <th>Status</th>
                                  <th></th>
                                </tr>
                              </thead>
                              <tbody>
                                {wfs.map((wf) => (
                                  <tr key={wf.id}>
                                    <td>{wf.url}</td>
                                    <td>{wf.apiKey}</td>
                                    <td>{wf.enabled ? 'Enabled' : 'Disabled'}</td>
                                    <td>
                                      <div className="vorn-button-group">
                                        <button
                                          type="button"
                                          onClick={() => handleToggleWebdavEnabled(l.id, wf)}
                                        >
                                          {wf.enabled ? 'Disable' : 'Enable'}
                                        </button>
                                        <button
                                          type="button"
                                          className="vorn-btn-danger"
                                          onClick={() => handleDeleteWebdav(l.id, wf)}
                                        >
                                          Remove
                                        </button>
                                      </div>
                                    </td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </div>
                        )}
                        <form className="vorn-inline-form" onSubmit={handleAddWebdav}>
                          <input
                            placeholder="WebDAV URL (default: https://webdav.torbox.app)"
                            value={webdavUrl}
                            onChange={(e) => setWebdavUrl(e.target.value)}
                            style={{ minWidth: '22rem' }}
                          />
                          <input
                            placeholder="TorBox API key"
                            value={webdavApiKey}
                            onChange={(e) => setWebdavApiKey(e.target.value)}
                            required
                            style={{ minWidth: '18rem' }}
                          />
                          <button type="submit" disabled={webdavSubmitting}>
                            {webdavSubmitting ? 'Adding…' : 'Add WebDAV folder'}
                          </button>
                        </form>
                      </div>
                    </td>
                  </tr>
                )}
              </Fragment>
            )
          })}
        </tbody>
        </table>
        </div>
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Add library</h2>
        </div>
        <form className="vorn-inline-form" onSubmit={handleCreate}>
          <input placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} required />
          <Select
            value={type}
            onChange={(v) => {
              setType(v as LibraryType)
              if (v !== 'movie' && v !== 'series') setIs4K(false)
            }}
            options={[
              { value: 'movie', label: 'Movies' },
              { value: 'series', label: 'Series' },
              { value: 'music', label: 'Music' },
              { value: 'audiobook', label: 'Audiobooks' },
            ]}
          />
          <input
            placeholder={
              type === 'movie' || type === 'series'
                ? 'Folder path on the server (optional for a debrid-only library)'
                : 'Folder path on the server'
            }
            value={folder}
            onChange={(e) => setFolder(e.target.value)}
            style={{ minWidth: '16rem' }}
            required={type === 'music' || type === 'audiobook'}
          />
          {(type === 'movie' || type === 'series') && (
            <label title="Marks this library as 4K-only and sets its quality profile to only fetch 2160p releases">
              <input type="checkbox" checked={is4K} onChange={(e) => setIs4K(e.target.checked)} /> 4K only
            </label>
          )}
          <button type="button" onClick={() => setBrowserOpen(true)}>
            <FolderIcon style={{ verticalAlign: '-0.15em', marginRight: '0.35rem' }} />
            Browse…
          </button>
          <button type="submit" disabled={submitting}>
            {submitting ? 'Adding…' : 'Add library'}
          </button>
        </form>
        {(type === 'music' || type === 'audiobook') && (
          <p className="vorn-panel-subtitle" style={{ margin: '0.75rem 0 0' }}>
            Title/artist/album metadata is read from each file's embedded tags — there's no external
            metadata/cover-art provider for {type === 'music' ? 'music' : 'audiobooks'} yet, so items will show a
            plain fallback poster until one is added.
          </p>
        )}
        {(type === 'movie' || type === 'series') && !folder && (
          <p className="vorn-panel-subtitle" style={{ margin: '0.75rem 0 0' }}>
            No folder means nothing to scan from disk — fine for a library meant purely as a debrid acquisition
            target (e.g. one you'll set as the default target for content requests), since debrid-resolved items
            stream directly from the provider and never touch a local folder. You can also add a WebDAV source
            above to populate the library from a WebDAV server like TorBox.
          </p>
        )}
      </div>

      {browserOpen && (
        <DirectoryBrowser
          initialPath={folder || undefined}
          onClose={() => setBrowserOpen(false)}
          onSelect={(path) => {
            setFolder(path)
            setBrowserOpen(false)
          }}
        />
      )}
    </section>
  )
}

import { useEffect, useState, type FormEvent } from 'react'
import {
  ApiError,
  createLibrary,
  deleteLibrary,
  getMetadataJob,
  getScanJob,
  listLibraries,
  startLibraryScan,
  startMetadataSync,
  type Library,
  type LibraryType,
  type MetadataJob,
  type ScanJob,
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
  const [folder, setFolder] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [browserOpen, setBrowserOpen] = useState(false)

  async function refresh() {
    setLibraries(await listLibraries())
  }

  useEffect(() => {
    refresh().catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await createLibrary({ name, type, folders: [folder] })
      setName('')
      setFolder('')
      await refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to create library')
    } finally {
      setSubmitting(false)
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
            <th>Name</th>
            <th>Type</th>
            <th>Folders</th>
            <th>Scan status</th>
            <th>Metadata status</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {libraries.map((l) => {
            const scanJob = scanJobs[l.id]
            const metaJob = metadataJobs[l.id]
            return (
              <tr key={l.id}>
                <td>{l.name}</td>
                <td>{l.type}</td>
                <td>{l.folders.join(', ')}</td>
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
                    <button type="button" className="vorn-btn-danger" onClick={() => handleDelete(l.id)}>
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
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
            onChange={(v) => setType(v as LibraryType)}
            options={[
              { value: 'movie', label: 'Movies' },
              { value: 'series', label: 'Series' },
              { value: 'music', label: 'Music' },
              { value: 'audiobook', label: 'Audiobooks' },
            ]}
          />
          <input
            placeholder="Folder path on the server"
            value={folder}
            onChange={(e) => setFolder(e.target.value)}
            style={{ minWidth: '16rem' }}
            required
          />
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
            Music and audiobook libraries can be created, but scanning only recognizes video files today — this
            library will show 0 items until audio scanning and metadata matching are added.
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

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
  type MetadataJob,
  type ScanJob,
} from '../api/client'
import './AdminUsers.css'

export function AdminLibraries() {
  const [libraries, setLibraries] = useState<Library[]>([])
  const [scanJobs, setScanJobs] = useState<Record<string, ScanJob>>({})
  const [metadataJobs, setMetadataJobs] = useState<Record<string, MetadataJob>>({})
  const [error, setError] = useState<string | null>(null)

  const [name, setName] = useState('')
  const [type, setType] = useState<'movie' | 'series'>('movie')
  const [folder, setFolder] = useState('')
  const [submitting, setSubmitting] = useState(false)

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
    <section className="vorn-admin-users">
      <h1>Libraries</h1>
      {error && <p className="vorn-form-error">{error}</p>}

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
                  <button type="button" onClick={() => handleScan(l.id)} disabled={scanJob?.status === 'running'}>
                    {scanJob?.status === 'running' ? 'Scanning…' : 'Scan'}
                  </button>{' '}
                  <button
                    type="button"
                    onClick={() => handleSyncMetadata(l.id)}
                    disabled={metaJob?.status === 'running'}
                  >
                    {metaJob?.status === 'running' ? 'Syncing…' : 'Sync metadata'}
                  </button>{' '}
                  <button type="button" onClick={() => handleDelete(l.id)}>
                    Delete
                  </button>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      </div>

      <h2>Add library</h2>
      <form className="vorn-inline-form" onSubmit={handleCreate}>
        <input placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} required />
        <select value={type} onChange={(e) => setType(e.target.value as 'movie' | 'series')}>
          <option value="movie">Movies</option>
          <option value="series">Series</option>
        </select>
        <input
          placeholder="Folder path on the server"
          value={folder}
          onChange={(e) => setFolder(e.target.value)}
          required
        />
        <button type="submit" disabled={submitting}>
          {submitting ? 'Adding…' : 'Add library'}
        </button>
      </form>
    </section>
  )
}

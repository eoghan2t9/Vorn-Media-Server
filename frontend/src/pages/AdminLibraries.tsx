import { Fragment, useEffect, useState, type FormEvent } from 'react'
import {
  ApiError,
  createLibrary,
  deleteLibrary,
  getMetadataJob,
  getQualityProfile,
  getScanJob,
  listLibraries,
  startLibraryScan,
  startMetadataSync,
  updateQualityProfile,
  type Library,
  type LibraryType,
  type MetadataJob,
  type QualityProfile,
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

  const [qualityOpenFor, setQualityOpenFor] = useState<string | null>(null)
  const [qualityProfile, setQualityProfile] = useState<QualityProfile | null>(null)
  const [qualityLoading, setQualityLoading] = useState(false)
  const [qualitySaving, setQualitySaving] = useState(false)

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
              <Fragment key={l.id}>
                <tr>
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
                      {(l.type === 'movie' || l.type === 'series') && (
                        <button type="button" onClick={() => handleToggleQuality(l.id)}>
                          {qualityOpenFor === l.id ? 'Hide quality profile' : 'Quality profile'}
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
                    <td colSpan={6}>
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
            Title/artist/album metadata is read from each file's embedded tags — there's no external
            metadata/cover-art provider for {type === 'music' ? 'music' : 'audiobooks'} yet, so items will show a
            plain fallback poster until one is added.
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

import { useEffect, useRef, useState, type FormEvent } from 'react'
import {
  ApiError,
  addNZBFile,
  createUsenetServer,
  deleteUsenetServer,
  listLibraries,
  listNZBDownloads,
  listUsenetServers,
  removeNZBDownload,
  testUsenetServer,
  type Library,
  type NZBDownload,
  type UsenetServer,
} from '../api/client'
import { FileDropzone, type FileDropzoneHandle } from '../components/FileDropzone'
import { Select } from '../components/Select'
import './AdminUsers.css'

function formatBytes(n: number) {
  if (n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  return `${(n / 1024 ** i).toFixed(1)} ${units[i]}`
}

export function AdminNzb() {
  const [downloads, setDownloads] = useState<NZBDownload[]>([])
  const [libraries, setLibraries] = useState<Library[]>([])
  const [servers, setServers] = useState<UsenetServer[]>([])
  const [error, setError] = useState<string | null>(null)

  const [libraryId, setLibraryId] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const dropzoneRef = useRef<FileDropzoneHandle>(null)

  const [serverName, setServerName] = useState('')
  const [serverHost, setServerHost] = useState('')
  const [serverPort, setServerPort] = useState('563')
  const [serverUseTls, setServerUseTls] = useState(true)
  const [serverUsername, setServerUsername] = useState('')
  const [serverPassword, setServerPassword] = useState('')
  const [serverMaxConnections, setServerMaxConnections] = useState('10')
  const [testingServer, setTestingServer] = useState(false)
  const [serverTestResult, setServerTestResult] = useState<{ ok: boolean; message: string } | null>(null)

  async function refreshDownloads() {
    setDownloads(await listNZBDownloads())
  }

  useEffect(() => {
    Promise.all([refreshDownloads(), listLibraries().then(setLibraries), listUsenetServers().then(setServers)]).catch(
      (err) => setError(err instanceof ApiError ? err.message : String(err)),
    )
    const interval = setInterval(() => {
      refreshDownloads().catch(() => {})
    }, 2000)
    return () => clearInterval(interval)
  }, [])

  async function handleFileSelected(file: File) {
    setError(null)
    setSubmitting(true)
    try {
      await addNZBFile(file, { libraryId: libraryId || undefined })
      await refreshDownloads()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add nzb file')
    } finally {
      setSubmitting(false)
      dropzoneRef.current?.reset()
    }
  }

  async function handleRemove(id: string, deleteFiles: boolean) {
    try {
      await removeNZBDownload(id, deleteFiles)
      await refreshDownloads()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to remove nzb download')
    }
  }

  async function handleAddServer(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const server = await createUsenetServer({
        name: serverName,
        host: serverHost,
        port: Number(serverPort),
        useTls: serverUseTls,
        username: serverUsername || undefined,
        password: serverPassword || undefined,
        maxConnections: Number(serverMaxConnections),
      })
      setServers((list) => [...list, server])
      setServerName('')
      setServerHost('')
      setServerUsername('')
      setServerPassword('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add usenet server')
    }
  }

  async function handleTestServer() {
    setServerTestResult(null)
    setTestingServer(true)
    try {
      const result = await testUsenetServer({
        host: serverHost,
        port: Number(serverPort),
        useTls: serverUseTls,
        username: serverUsername || undefined,
        password: serverPassword || undefined,
      })
      setServerTestResult(
        result.ok ? { ok: true, message: 'Connected and authenticated successfully.' } : { ok: false, message: result.error ?? 'Connection failed.' },
      )
    } catch (err) {
      setServerTestResult({ ok: false, message: err instanceof ApiError ? err.message : 'Failed to test connection' })
    } finally {
      setTestingServer(false)
    }
  }

  async function handleDeleteServer(id: string) {
    try {
      await deleteUsenetServer(id)
      setServers((list) => list.filter((s) => s.id !== id))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete usenet server')
    }
  }

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>NZB / Usenet</h1>
        <p className="vorn-admin-page-subtitle">Download .nzb files through a configured Usenet server.</p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Downloads</h2>
        </div>
        <div className="vorn-table-wrap">
        <table className="vorn-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>Progress</th>
              <th>Promoted</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {downloads.map((n) => (
              <tr key={n.id}>
                <td>{n.name}</td>
                <td>{n.status === 'error' ? `error: ${n.error}` : n.status}</td>
                <td>
                  {formatBytes(n.bytesDone)} / {formatBytes(n.bytesTotal)}
                  {n.bytesTotal > 0 ? ` (${Math.floor((100 * n.bytesDone) / n.bytesTotal)}%)` : ''}
                </td>
                <td>{n.promoted ? 'yes' : 'no'}</td>
                <td>
                  <div className="vorn-button-group">
                    <button type="button" className="vorn-btn-danger" onClick={() => handleRemove(n.id, false)}>
                      Remove
                    </button>
                    <button type="button" className="vorn-btn-danger" onClick={() => handleRemove(n.id, true)}>
                      Remove + delete files
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Add NZB</h2>
        </div>
        <div className="vorn-inline-form" style={{ marginBottom: '0.75rem' }}>
          <Select
            value={libraryId}
            onChange={setLibraryId}
            placeholder="No destination library"
            options={libraries.map((l) => ({ value: l.id, label: l.name }))}
          />
        </div>
        <FileDropzone
          ref={dropzoneRef}
          accept=".nzb"
          hint=".nzb files"
          disabled={submitting}
          onFile={handleFileSelected}
        />
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Usenet servers</h2>
        </div>
        <div className="vorn-table-wrap">
        <table className="vorn-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Host</th>
              <th>Port</th>
              <th>TLS</th>
              <th>Max conns</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {servers.map((s) => (
              <tr key={s.id}>
                <td>{s.name}</td>
                <td>{s.host}</td>
                <td>{s.port}</td>
                <td>{s.useTls ? 'yes' : 'no'}</td>
                <td>{s.maxConnections}</td>
                <td>
                  <button type="button" className="vorn-btn-danger" onClick={() => handleDeleteServer(s.id)}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
        <form className="vorn-inline-form" onSubmit={handleAddServer} style={{ marginTop: '1rem' }}>
          <input placeholder="Name" value={serverName} onChange={(e) => setServerName(e.target.value)} required />
          <input placeholder="Host" value={serverHost} onChange={(e) => setServerHost(e.target.value)} required />
          <input
            placeholder="Port"
            type="number"
            value={serverPort}
            onChange={(e) => setServerPort(e.target.value)}
            style={{ width: '6rem' }}
            required
          />
          <label>
            <input type="checkbox" checked={serverUseTls} onChange={(e) => setServerUseTls(e.target.checked)} /> TLS
          </label>
          <input placeholder="Username (optional)" value={serverUsername} onChange={(e) => setServerUsername(e.target.value)} />
          <input
            placeholder="Password (optional)"
            type="password"
            value={serverPassword}
            onChange={(e) => setServerPassword(e.target.value)}
          />
          <input
            placeholder="Max connections"
            type="number"
            value={serverMaxConnections}
            onChange={(e) => setServerMaxConnections(e.target.value)}
            style={{ width: '8rem' }}
          />
          <button type="button" onClick={handleTestServer} disabled={testingServer || !serverHost || !serverPort}>
            {testingServer ? 'Testing…' : 'Test'}
          </button>
          <button type="submit">Add server</button>
        </form>
        {serverTestResult && (
          <p className={serverTestResult.ok ? 'vorn-test-result-ok' : 'vorn-form-error'} style={{ marginTop: '0.6rem' }}>
            {serverTestResult.message}
          </p>
        )}
      </div>
    </section>
  )
}

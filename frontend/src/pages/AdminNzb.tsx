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
  type Library,
  type NZBDownload,
  type UsenetServer,
} from '../api/client'
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
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [serverName, setServerName] = useState('')
  const [serverHost, setServerHost] = useState('')
  const [serverPort, setServerPort] = useState('563')
  const [serverUseTls, setServerUseTls] = useState(true)
  const [serverUsername, setServerUsername] = useState('')
  const [serverPassword, setServerPassword] = useState('')
  const [serverMaxConnections, setServerMaxConnections] = useState('10')

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

  async function handleFileChange() {
    const file = fileInputRef.current?.files?.[0]
    if (!file) return
    setError(null)
    setSubmitting(true)
    try {
      await addNZBFile(file, { libraryId: libraryId || undefined })
      await refreshDownloads()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add nzb file')
    } finally {
      setSubmitting(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
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

  async function handleDeleteServer(id: string) {
    try {
      await deleteUsenetServer(id)
      setServers((list) => list.filter((s) => s.id !== id))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete usenet server')
    }
  }

  return (
    <section className="vorn-admin-users">
      <h1>NZB / Usenet</h1>
      {error && <p className="vorn-form-error">{error}</p>}

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
                <button type="button" onClick={() => handleRemove(n.id, false)}>
                  Remove
                </button>{' '}
                <button type="button" onClick={() => handleRemove(n.id, true)}>
                  Remove + delete files
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>

      <h2>Add NZB</h2>
      <p>
        <select value={libraryId} onChange={(e) => setLibraryId(e.target.value)}>
          <option value="">No destination library (won't auto-add)</option>
          {libraries.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </select>{' '}
        Upload a .nzb file: <input ref={fileInputRef} type="file" accept=".nzb" onChange={handleFileChange} disabled={submitting} />
      </p>

      <h2>Usenet servers</h2>
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
                <button type="button" onClick={() => handleDeleteServer(s.id)}>
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>
      <form className="vorn-inline-form" onSubmit={handleAddServer}>
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
        <button type="submit">Add server</button>
      </form>
    </section>
  )
}

import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { fetchTranscodeCapabilities } from '../api/client'

export function AdminHome() {
  const [backends, setBackends] = useState<string[] | null>(null)

  useEffect(() => {
    fetchTranscodeCapabilities()
      .then((c) => setBackends(c.backends ?? []))
      .catch(() => setBackends([]))
  }, [])

  return (
    <section>
      <h1>Admin</h1>
      <p>
        <Link to="/admin/libraries">Manage libraries →</Link>
      </p>
      <p>
        <Link to="/admin/users">Manage users →</Link>
      </p>
      <p>
        <Link to="/admin/currently-watching">Currently watching →</Link>
      </p>
      <p>
        <Link to="/admin/torrents">Torrents →</Link>
      </p>
      <p>
        <Link to="/admin/nzb">NZB / Usenet →</Link>
      </p>
      <p>
        <Link to="/admin/debrid">Debrid →</Link>
      </p>
      <p>
        <Link to="/admin/logs">Logs →</Link>
      </p>

      <h2>Transcoder</h2>
      {backends === null ? (
        <p>Checking…</p>
      ) : backends.length === 0 ? (
        <p>No working encoder detected (ffmpeg missing, or no hardware/software backend probed successfully).</p>
      ) : (
        <p>
          Detected, real-probe-verified backends:{' '}
          {backends.map((b) => (
            <code key={b} style={{ marginRight: '0.5rem' }}>
              {b}
            </code>
          ))}
        </p>
      )}
    </section>
  )
}

import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { fetchTranscodeCapabilities } from '../api/client'
import './AdminHome.css'

const TILES = [
  { to: '/admin/libraries', label: 'Manage libraries' },
  { to: '/admin/users', label: 'Manage users' },
  { to: '/admin/currently-watching', label: 'Currently watching' },
  { to: '/admin/torrents', label: 'Torrents' },
  { to: '/admin/nzb', label: 'NZB / Usenet' },
  { to: '/admin/debrid', label: 'Debrid' },
  { to: '/admin/logs', label: 'Logs' },
  { to: '/admin/server-settings', label: 'Network' },
]

export function AdminHome() {
  const [backends, setBackends] = useState<string[] | null>(null)

  useEffect(() => {
    fetchTranscodeCapabilities()
      .then((c) => setBackends(c.backends ?? []))
      .catch(() => setBackends([]))
  }, [])

  return (
    <section className="vorn-admin-home">
      <h1>Admin</h1>

      <div className="vorn-admin-tiles">
        {TILES.map((tile) => (
          <Link key={tile.to} to={tile.to} className="vorn-admin-tile">
            {tile.label}
            <span className="vorn-admin-tile-arrow" aria-hidden>
              →
            </span>
          </Link>
        ))}
      </div>

      <div className="vorn-admin-panel">
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
      </div>
    </section>
  )
}

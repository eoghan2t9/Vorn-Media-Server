import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  fetchTranscodeCapabilities,
  listCurrentlyWatching,
  listLibraries,
  listUsers,
  restartServer,
} from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import {
  CloudDownloadIcon,
  CloudIcon,
  DashboardIcon,
  EyeIcon,
  GlobeIcon,
  LibraryIcon,
  MagnetIcon,
  PlugIcon,
  TerminalIcon,
  UsersIcon,
} from '../components/icons'
import './AdminHome.css'

const TILES = [
  { to: '/admin/libraries', label: 'Libraries', desc: 'Folders, scans, metadata sync', icon: LibraryIcon },
  { to: '/admin/users', label: 'Users', desc: 'Accounts and library access', icon: UsersIcon },
  { to: '/admin/currently-watching', label: 'Currently watching', desc: 'Live playback sessions', icon: EyeIcon },
  { to: '/admin/torrents', label: 'Torrents', desc: 'Magnets, files, indexers', icon: MagnetIcon },
  { to: '/admin/nzb', label: 'NZB / Usenet', desc: 'Usenet servers and downloads', icon: CloudDownloadIcon },
  { to: '/admin/debrid', label: 'Debrid', desc: 'Real-Debrid / TorBox accounts', icon: CloudIcon },
  { to: '/admin/integrations', label: 'Integrations', desc: 'TMDb, OpenSubtitles', icon: PlugIcon },
  { to: '/admin/server-settings', label: 'Network', desc: 'Domain, HTTPS, updates', icon: GlobeIcon },
  { to: '/admin/logs', label: 'Logs', desc: 'Live tail and maintenance', icon: TerminalIcon },
]

export function AdminHome() {
  const [backends, setBackends] = useState<string[] | null>(null)
  const [userCount, setUserCount] = useState<number | null>(null)
  const [libraryCount, setLibraryCount] = useState<number | null>(null)
  const [watchingCount, setWatchingCount] = useState<number | null>(null)

  const [restarting, setRestarting] = useState(false)
  const [restartConfirmOpen, setRestartConfirmOpen] = useState(false)
  const [restartMessage, setRestartMessage] = useState<string | null>(null)

  useEffect(() => {
    fetchTranscodeCapabilities()
      .then((c) => setBackends(c.backends ?? []))
      .catch(() => setBackends([]))
    listUsers()
      .then((u) => setUserCount(u.length))
      .catch(() => setUserCount(null))
    listLibraries()
      .then((l) => setLibraryCount(l.length))
      .catch(() => setLibraryCount(null))
    listCurrentlyWatching()
      .then((w) => setWatchingCount(w.length))
      .catch(() => setWatchingCount(null))
  }, [])

  const transcoderReady = backends !== null && backends.length > 0

  async function handleRestart() {
    setRestarting(true)
    setRestartMessage(null)
    try {
      await restartServer()
    } catch {
      // Expected more often than not: the process typically exits before
      // this request's response ever finishes, which surfaces here as a
      // network error rather than a clean reply -- not a real failure.
    }
    setRestartConfirmOpen(false)
    setRestarting(false)
    setRestartMessage('Restart initiated. The server should be back within a few seconds; this page will stop responding until then.')
  }

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Dashboard</h1>
        <p className="vorn-admin-page-subtitle">Overview of your Vorn instance.</p>
      </div>

      <div className="vorn-stat-grid">
        <div className="vorn-stat-card">
          <UsersIcon className="vorn-stat-icon" />
          <div className="vorn-stat-value">{userCount ?? '—'}</div>
          <div className="vorn-stat-label">Users</div>
        </div>
        <div className="vorn-stat-card">
          <LibraryIcon className="vorn-stat-icon" />
          <div className="vorn-stat-value">{libraryCount ?? '—'}</div>
          <div className="vorn-stat-label">Libraries</div>
        </div>
        <div className="vorn-stat-card">
          <EyeIcon className="vorn-stat-icon" />
          <div className="vorn-stat-value">{watchingCount ?? '—'}</div>
          <div className="vorn-stat-label">Currently watching</div>
        </div>
        <div className="vorn-stat-card">
          <DashboardIcon className="vorn-stat-icon" />
          <div className="vorn-stat-value vorn-stat-value-status">
            <span className={`vorn-status-dot${transcoderReady ? ' vorn-status-dot-on' : ''}`} />
            {backends === null ? 'Checking…' : transcoderReady ? 'Ready' : 'Not ready'}
          </div>
          <div className="vorn-stat-label">Transcoder</div>
        </div>
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Transcoder backends</h2>
        </div>
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

      <div className="vorn-panel-header" style={{ marginBottom: '0.9rem' }}>
        <h2>Manage</h2>
      </div>
      <div className="vorn-admin-tiles">
        {TILES.map((tile) => (
          <Link key={tile.to} to={tile.to} className="vorn-admin-tile">
            <tile.icon className="vorn-admin-tile-icon" />
            <div className="vorn-admin-tile-text">
              <div className="vorn-admin-tile-label">{tile.label}</div>
              <div className="vorn-admin-tile-desc">{tile.desc}</div>
            </div>
            <span className="vorn-admin-tile-arrow" aria-hidden>
              →
            </span>
          </Link>
        ))}
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Server</h2>
        </div>
        <p className="vorn-panel-subtitle">
          Restarts the Vorn process immediately, disconnecting every active stream and admin session. Under the
          standard Docker deployment it comes right back up on its own; on a native/systemd install, make sure
          something is actually configured to restart the process, or it will just stay down.
        </p>
        <button type="button" className="vorn-btn-danger" onClick={() => setRestartConfirmOpen(true)} disabled={restarting}>
          {restarting ? 'Restarting…' : 'Restart server'}
        </button>
        {restartMessage && <p>{restartMessage}</p>}
      </div>

      {restartConfirmOpen && (
        <ConfirmDialog
          title="Restart server?"
          message="This immediately disconnects every active stream and admin session for everyone using this server. It should come back within a few seconds under the standard Docker setup."
          confirmLabel="Restart"
          danger
          busy={restarting}
          onConfirm={handleRestart}
          onCancel={() => setRestartConfirmOpen(false)}
        />
      )}
    </section>
  )
}

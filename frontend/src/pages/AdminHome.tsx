import { useEffect, useState, type ReactNode } from 'react'
import {
  fetchSystemStats,
  fetchTranscodeCapabilities,
  listCurrentlyWatching,
  listLibraries,
  listUsers,
  restartServer,
  type SystemStats,
} from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import {
  CpuIcon,
  DashboardIcon,
  EyeIcon,
  HardDriveIcon,
  LibraryIcon,
  MemoryIcon,
  NetworkIcon,
  UsersIcon,
} from '../components/icons'
import './AdminHome.css'

const STATS_POLL_INTERVAL_MS = 3000

function formatBytes(n: number) {
  if (n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  return `${(n / 1024 ** i).toFixed(1)} ${units[i]}`
}

function formatRate(bytesPerSec: number) {
  return `${formatBytes(bytesPerSec)}/s`
}

function LoadStatCard({
  icon: Icon,
  label,
  percent,
  detail,
}: {
  icon: (props: { className?: string }) => ReactNode
  label: string
  percent: number | null
  detail?: string
}) {
  return (
    <div className="vorn-stat-card">
      <Icon className="vorn-stat-icon" />
      <div className="vorn-stat-value">{percent === null ? '—' : `${percent.toFixed(0)}%`}</div>
      <div className="vorn-stat-label">{label}</div>
      {percent !== null && (
        <div className="vorn-load-bar">
          <div
            className={`vorn-load-bar-fill${percent >= 90 ? ' vorn-load-bar-fill-hot' : ''}`}
            style={{ width: `${Math.min(100, percent)}%` }}
          />
        </div>
      )}
      {detail && <div className="vorn-stat-detail">{detail}</div>}
    </div>
  )
}

function NetworkStatCard({ available, rxBytesPerSec, txBytesPerSec }: { available: boolean; rxBytesPerSec: number; txBytesPerSec: number }) {
  return (
    <div className="vorn-stat-card">
      <NetworkIcon className="vorn-stat-icon" />
      <div className="vorn-stat-value vorn-stat-value-network">{available ? `↓ ${formatRate(rxBytesPerSec)}` : '—'}</div>
      <div className="vorn-stat-label">Network</div>
      {available && <div className="vorn-stat-detail">↑ {formatRate(txBytesPerSec)}</div>}
    </div>
  )
}

export function AdminHome() {
  const [backends, setBackends] = useState<string[] | null>(null)
  const [userCount, setUserCount] = useState<number | null>(null)
  const [libraryCount, setLibraryCount] = useState<number | null>(null)
  const [watchingCount, setWatchingCount] = useState<number | null>(null)
  const [sysStats, setSysStats] = useState<SystemStats | null>(null)

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

  useEffect(() => {
    function refresh() {
      fetchSystemStats()
        .then(setSysStats)
        .catch(() => setSysStats(null))
    }
    refresh()
    const interval = setInterval(refresh, STATS_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [])

  const transcoderReady = backends !== null && backends.length > 0
  const memPercent =
    sysStats?.memAvailable && sysStats.memTotalBytes > 0 ? (sysStats.memUsedBytes / sysStats.memTotalBytes) * 100 : null
  const diskPercent =
    sysStats?.diskAvailable && sysStats.diskTotalBytes > 0
      ? (sysStats.diskUsedBytes / sysStats.diskTotalBytes) * 100
      : null
  const anyStatUnavailable =
    sysStats !== null && (!sysStats.cpuAvailable || !sysStats.memAvailable || !sysStats.diskAvailable || !sysStats.netAvailable)

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

      <div className="vorn-panel-header" style={{ marginBottom: '0.9rem' }}>
        <h2>Server load</h2>
      </div>
      <div className="vorn-stat-grid">
        <LoadStatCard icon={CpuIcon} label="CPU" percent={sysStats?.cpuAvailable ? sysStats.cpuPercent : null} />
        <LoadStatCard
          icon={MemoryIcon}
          label="Memory"
          percent={memPercent}
          detail={sysStats?.memAvailable ? `${formatBytes(sysStats.memUsedBytes)} / ${formatBytes(sysStats.memTotalBytes)}` : undefined}
        />
        <LoadStatCard
          icon={HardDriveIcon}
          label="Storage"
          percent={diskPercent}
          detail={sysStats?.diskAvailable ? `${formatBytes(sysStats.diskUsedBytes)} / ${formatBytes(sysStats.diskTotalBytes)}` : undefined}
        />
        <NetworkStatCard
          available={sysStats?.netAvailable ?? false}
          rxBytesPerSec={sysStats?.netRxBytesPerSec ?? 0}
          txBytesPerSec={sysStats?.netTxBytesPerSec ?? 0}
        />
      </div>
      {anyStatUnavailable && (
        <p className="vorn-panel-subtitle" style={{ margin: '-0.75rem 0 1.5rem' }}>
          Some stats aren't available on this host's OS — CPU/memory/disk/network reporting varies by platform, and
          this instance is running on one where not everything can be read.
        </p>
      )}

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

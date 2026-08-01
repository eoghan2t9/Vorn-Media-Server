import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, listCurrentlyWatching, setItemMonitored, type CurrentlyWatchingEntry } from '../api/client'
import './AdminUsers.css'

const POLL_INTERVAL_MS = 5000

// quickRes parses a release title into a short quality label.
function quickRes(r: string) {
  const m = r.match(/\b(2160p|4k|1080p|720p|480p)\b/i)
  return m ? (m[1].toLowerCase() === '4k' ? '4K' : m[1]) : ''
}

export function AdminCurrentlyWatching() {
  const [entries, setEntries] = useState<CurrentlyWatchingEntry[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    function refresh() {
      listCurrentlyWatching()
        .then((e) => !cancelled && setEntries(e))
        .catch((err) => !cancelled && setError(err instanceof ApiError ? err.message : String(err)))
    }
    refresh()
    const interval = setInterval(refresh, POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [])

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Currently Watching</h1>
        <p className="vorn-admin-page-subtitle">Live view of active playback sessions, refreshed every 5 seconds.</p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      <div className="vorn-panel">
        {entries.length === 0 ? (
          <p className="vorn-empty">Nobody is watching anything right now.</p>
        ) : (
          <div className="vorn-table-wrap">
          <table className="vorn-table">
            <thead>
              <tr>
                <th>User</th>
                <th>Title</th>
                <th>Quality</th>
                <th>Progress</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => {
                const pct = e.durationSeconds > 0 ? Math.round((e.positionSeconds / e.durationSeconds) * 100) : 0
                const res = e.item.currentReleaseTitle ? quickRes(e.item.currentReleaseTitle) : ''
                return (
                  <tr key={`${e.username}-${e.item.id}`}>
                    <td>{e.username}</td>
                    <td>
                      <Link to={`/items/${e.item.id}`}>{e.item.title}</Link>
                    </td>
                    <td>{res || '—'}</td>
                    <td>{pct}%</td>
                    <td>
                      {e.item.acquisitionStatus === 'owned' && (
                        <button
                          type="button"
                          className="vorn-card-monitor-btn"
                          onClick={async () => {
                            try {
                              const updated = await setItemMonitored(e.item.id, !e.item.monitored)
                              setEntries((prev) =>
                                prev.map((p) =>
                                  p.item.id === updated.id ? { ...p, item: updated } : p,
                                ),
                              )
                            } catch {}
                          }}
                          title={e.item.monitored ? 'Stop watching for upgrades' : 'Watch for better releases'}
                        >
                          {e.item.monitored ? '★' : '☆'}
                        </button>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
          </div>
        )}
      </div>
    </section>
  )
}

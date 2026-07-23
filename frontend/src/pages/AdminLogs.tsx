import { useEffect, useRef, useState } from 'react'
import { ApiError, clearScanCache, clearTranscodeCache, fetchSubtitlesQuota, logsStreamUrl, type SubtitlesQuota } from '../api/client'
import './AdminUsers.css'

const MAX_LINES = 2000

export function AdminLogs() {
  const [lines, setLines] = useState<string[]>([])
  const [connected, setConnected] = useState(false)
  const [maintenanceMsg, setMaintenanceMsg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [quota, setQuota] = useState<SubtitlesQuota | null>(null)
  const logRef = useRef<HTMLPreElement>(null)
  const autoScroll = useRef(true)

  useEffect(() => {
    const ws = new WebSocket(logsStreamUrl())
    ws.onopen = () => setConnected(true)
    ws.onclose = () => setConnected(false)
    ws.onerror = () => setError('Log stream disconnected')
    ws.onmessage = (evt) => {
      setLines((prev) => {
        const next = [...prev, evt.data as string]
        return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
      })
    }
    return () => ws.close()
  }, [])

  useEffect(() => {
    fetchSubtitlesQuota()
      .then(setQuota)
      .catch(() => setQuota(null))
  }, [])

  useEffect(() => {
    if (autoScroll.current && logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [lines])

  function handleScroll() {
    const el = logRef.current
    if (!el) return
    autoScroll.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
  }

  async function handleClearScanCache() {
    setMaintenanceMsg(null)
    setError(null)
    try {
      const result = await clearScanCache()
      setMaintenanceMsg(`Cleared ${result.cleared} stale scan-staging key(s).`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to clear scan cache')
    }
  }

  async function handleClearTranscodeCache() {
    setMaintenanceMsg(null)
    setError(null)
    try {
      const result = await clearTranscodeCache()
      setMaintenanceMsg(`Cleared ${result.cleared} finished transcode session(s).`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to clear transcode cache')
    }
  }

  return (
    <section className="vorn-admin-users">
      <h1>Logs</h1>
      {error && <p className="vorn-form-error">{error}</p>}
      {maintenanceMsg && <p>{maintenanceMsg}</p>}

      <p>
        Live tail: <strong>{connected ? 'connected' : 'disconnected'}</strong>
      </p>
      <pre className="vorn-log-viewer" ref={logRef} onScroll={handleScroll}>
        {lines.join('\n')}
      </pre>

      <h2>Maintenance</h2>
      <p>
        <button type="button" onClick={handleClearScanCache}>
          Clear scan cache
        </button>{' '}
        <button type="button" onClick={handleClearTranscodeCache}>
          Clear finished transcode sessions
        </button>
      </p>

      <h2>OpenSubtitles quota</h2>
      {quota === null ? (
        <p>Checking…</p>
      ) : quota.remaining < 0 ? (
        <p>Not configured (set VORN_OPENSUBTITLES_API_KEY and VORN_OPENSUBTITLES_USERNAME).</p>
      ) : (
        <p>
          {quota.remaining} download(s) remaining today{quota.resetTime ? ` (resets in ${quota.resetTime})` : ''}.
        </p>
      )}
    </section>
  )
}

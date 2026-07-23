import { useEffect, useState, type FormEvent } from 'react'
import {
  ApiError,
  applyUpdate,
  checkForUpdate,
  fetchServerSettings,
  updateServerSettings,
  type UpdateCheckResult,
} from '../api/client'
import './AdminUsers.css'

export function AdminServerSettings() {
  const [customDomain, setCustomDomain] = useState('')
  const [acmeEmail, setAcmeEmail] = useState('')
  const [sslEnabled, setSslEnabled] = useState(false)
  const [trustCloudflare, setTrustCloudflare] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [updateInfo, setUpdateInfo] = useState<UpdateCheckResult | null>(null)
  const [updateBusy, setUpdateBusy] = useState(false)
  const [updateMessage, setUpdateMessage] = useState<string | null>(null)

  useEffect(() => {
    fetchServerSettings()
      .then((s) => {
        setCustomDomain(s.customDomain)
        setAcmeEmail(s.acmeEmail)
        setSslEnabled(s.sslEnabled)
        setTrustCloudflare(s.trustCloudflare)
        setLoaded(true)
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  async function handleCheckForUpdate() {
    setUpdateMessage(null)
    setUpdateBusy(true)
    try {
      setUpdateInfo(await checkForUpdate())
    } catch (err) {
      setUpdateMessage(err instanceof ApiError ? err.message : 'Failed to check for updates')
    } finally {
      setUpdateBusy(false)
    }
  }

  async function handleApplyUpdate() {
    setUpdateMessage(null)
    setUpdateBusy(true)
    try {
      const result = await applyUpdate()
      setUpdateInfo(result)
      setUpdateMessage(
        result.applied
          ? `Updated to ${result.latestVersion}. Restart the server to run it.`
          : 'No update was applied (already up to date, or none available).',
      )
    } catch (err) {
      setUpdateMessage(err instanceof ApiError ? err.message : 'Failed to apply update')
    } finally {
      setUpdateBusy(false)
    }
  }

  async function handleSave(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setMessage(null)
    setSaving(true)
    try {
      await updateServerSettings({ customDomain, acmeEmail, sslEnabled, trustCloudflare })
      setMessage(
        sslEnabled
          ? 'Saved. A custom domain/SSL change only takes effect after restarting the server.'
          : 'Saved.',
      )
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save settings')
    } finally {
      setSaving(false)
    }
  }

  if (!loaded) return <p>Loading…</p>

  return (
    <section className="vorn-admin-users">
      <h1>Network</h1>
      {error && <p className="vorn-form-error">{error}</p>}
      {message && <p>{message}</p>}

      <form className="vorn-inline-form" onSubmit={handleSave} style={{ flexDirection: 'column', alignItems: 'flex-start', gap: '0.75rem' }}>
        <label>
          Custom domain{' '}
          <input
            placeholder="media.example.com"
            value={customDomain}
            onChange={(e) => setCustomDomain(e.target.value)}
            style={{ minWidth: '16rem' }}
          />
        </label>
        <label>
          ACME contact email{' '}
          <input
            type="email"
            placeholder="admin@example.com"
            value={acmeEmail}
            onChange={(e) => setAcmeEmail(e.target.value)}
            style={{ minWidth: '16rem' }}
          />
        </label>
        <label>
          <input type="checkbox" checked={sslEnabled} onChange={(e) => setSslEnabled(e.target.checked)} /> Enable
          automatic HTTPS (Let's Encrypt via the custom domain above; requires ports 80 and 443 reachable
          from the internet for the domain)
        </label>
        <label>
          <input
            type="checkbox"
            checked={trustCloudflare}
            onChange={(e) => setTrustCloudflare(e.target.checked)}
          />{' '}
          Trust CF-Connecting-IP (only honored from requests that genuinely originate at a real Cloudflare
          edge IP, so it can't be spoofed by other clients)
        </label>
        <button type="submit" disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </form>

      <h2>Software update</h2>
      {updateMessage && <p>{updateMessage}</p>}
      <div className="vorn-button-group">
        <button type="button" onClick={handleCheckForUpdate} disabled={updateBusy}>
          Check for updates
        </button>
        {updateInfo && updateInfo.updateAvailable && !updateInfo.dockerized && (
          <button type="button" onClick={handleApplyUpdate} disabled={updateBusy}>
            {updateBusy ? 'Working…' : `Update to ${updateInfo.latestVersion}`}
          </button>
        )}
      </div>
      {updateInfo && (
        <p>
          Running {updateInfo.currentVersion}
          {updateInfo.latestVersion ? `; latest available is ${updateInfo.latestVersion}` : '; no release found'}.
          {updateInfo.dockerized && ' Running under Docker: rebuild/pull the image instead of self-updating.'}
        </p>
      )}
    </section>
  )
}

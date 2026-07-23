import { useEffect, useState, type FormEvent } from 'react'
import { ApiError, fetchServerSettings, updateServerSettings } from '../api/client'
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
    </section>
  )
}

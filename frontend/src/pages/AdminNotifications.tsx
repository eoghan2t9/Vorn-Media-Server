import { useEffect, useState, type FormEvent } from 'react'
import {
  ApiError,
  fetchNotificationSettings,
  sendTestNotification,
  updateNotificationSettings,
  type NotificationSettings,
} from '../api/client'
import './AdminUsers.css'

export function AdminNotifications() {
  const [settings, setSettings] = useState<NotificationSettings | null>(null)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchNotificationSettings()
      .then(setSettings)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  async function handleSave(e: FormEvent) {
    e.preventDefault()
    if (!settings) return
    setError(null)
    setSaving(true)
    try {
      setSettings(await updateNotificationSettings(settings))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save notification settings')
    } finally {
      setSaving(false)
    }
  }

  async function handleTest() {
    if (!settings?.webhookUrl) return
    setTestResult(null)
    setTesting(true)
    try {
      await sendTestNotification(settings.webhookUrl)
      setTestResult({ ok: true, message: 'Test notification sent -- check your webhook endpoint.' })
    } catch (err) {
      setTestResult({ ok: false, message: err instanceof ApiError ? err.message : 'Failed to send test notification' })
    } finally {
      setTesting(false)
    }
  }

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Notifications</h1>
        <p className="vorn-admin-page-subtitle">
          Vorn posts a JSON payload to this webhook URL when an on-demand acquisition succeeds or fails, and when a
          monitored item's quality is automatically upgraded. Works with ntfy, Discord, Slack, Home Assistant, or any
          endpoint that accepts a POST body.
        </p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      {settings && (
        <div className="vorn-panel">
          <div className="vorn-panel-header">
            <h2>Webhook</h2>
          </div>
          <form className="vorn-inline-form" onSubmit={handleSave}>
            <label>
              <input
                type="checkbox"
                checked={settings.enabled}
                onChange={(e) => setSettings({ ...settings, enabled: e.target.checked })}
              />{' '}
              Enabled
            </label>
            <input
              placeholder="https://ntfy.sh/your-topic"
              value={settings.webhookUrl}
              onChange={(e) => setSettings({ ...settings, webhookUrl: e.target.value })}
              style={{ minWidth: '20rem' }}
            />
            <button type="button" onClick={handleTest} disabled={testing || !settings.webhookUrl}>
              {testing ? 'Sending…' : 'Send test notification'}
            </button>
            <button type="submit" disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </button>
          </form>
          {testResult && (
            <p className={testResult.ok ? 'vorn-test-result-ok' : 'vorn-form-error'} style={{ marginTop: '0.6rem' }}>
              {testResult.message}
            </p>
          )}
        </div>
      )}
    </section>
  )
}

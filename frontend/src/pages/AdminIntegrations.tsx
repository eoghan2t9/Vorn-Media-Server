import { useEffect, useState, type FormEvent } from 'react'
import {
  ApiError,
  fetchIntegrationSettings,
  updateIntegrationSettings,
  type IntegrationSettings,
} from '../api/client'
import './AdminUsers.css'

function StatusBadge({ configured }: { configured: boolean }) {
  return (
    <span className={`vorn-status-badge ${configured ? 'vorn-status-badge-on' : 'vorn-status-badge-off'}`}>
      {configured ? 'Configured' : 'Not configured'}
    </span>
  )
}

export function AdminIntegrations() {
  const [settings, setSettings] = useState<IntegrationSettings | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [tmdbKeyInput, setTmdbKeyInput] = useState('')
  const [tmdbSaving, setTmdbSaving] = useState(false)
  const [tmdbMessage, setTmdbMessage] = useState<string | null>(null)

  const [osKeyInput, setOsKeyInput] = useState('')
  const [osUsername, setOsUsername] = useState('')
  const [osPasswordInput, setOsPasswordInput] = useState('')
  const [osSaving, setOsSaving] = useState(false)
  const [osMessage, setOsMessage] = useState<string | null>(null)

  const [musicSaving, setMusicSaving] = useState(false)
  const [audiobookSaving, setAudiobookSaving] = useState(false)

  function load() {
    fetchIntegrationSettings()
      .then((s) => {
        setSettings(s)
        setOsUsername(s.openSubtitlesUsername ?? '')
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }

  useEffect(load, [])

  async function handleSaveTmdb(e: FormEvent) {
    e.preventDefault()
    if (tmdbKeyInput.trim() === '') return
    setTmdbMessage(null)
    setTmdbSaving(true)
    try {
      const s = await updateIntegrationSettings({ tmdbApiKey: tmdbKeyInput.trim() })
      setSettings(s)
      setTmdbKeyInput('')
      setTmdbMessage('Saved. Restart the server for this to take effect.')
    } catch (err) {
      setTmdbMessage(err instanceof ApiError ? err.message : 'Failed to save')
    } finally {
      setTmdbSaving(false)
    }
  }

  async function handleClearTmdb() {
    setTmdbMessage(null)
    setTmdbSaving(true)
    try {
      const s = await updateIntegrationSettings({ tmdbApiKey: '' })
      setSettings(s)
      setTmdbKeyInput('')
      setTmdbMessage('Cleared. Restart the server for this to take effect.')
    } catch (err) {
      setTmdbMessage(err instanceof ApiError ? err.message : 'Failed to clear')
    } finally {
      setTmdbSaving(false)
    }
  }

  async function handleSaveOpenSubtitles(e: FormEvent) {
    e.preventDefault()
    setOsMessage(null)
    setOsSaving(true)
    try {
      const s = await updateIntegrationSettings({
        openSubtitlesApiKey: osKeyInput.trim() || undefined,
        openSubtitlesUsername: osUsername.trim(),
        openSubtitlesPassword: osPasswordInput || undefined,
      })
      setSettings(s)
      setOsKeyInput('')
      setOsPasswordInput('')
      setOsMessage('Saved. Restart the server for this to take effect.')
    } catch (err) {
      setOsMessage(err instanceof ApiError ? err.message : 'Failed to save')
    } finally {
      setOsSaving(false)
    }
  }

  async function handleClearOpenSubtitles() {
    setOsMessage(null)
    setOsSaving(true)
    try {
      const s = await updateIntegrationSettings({
        openSubtitlesApiKey: '',
        openSubtitlesUsername: '',
        openSubtitlesPassword: '',
      })
      setSettings(s)
      setOsKeyInput('')
      setOsUsername('')
      setOsPasswordInput('')
      setOsMessage('Cleared. Restart the server for this to take effect.')
    } catch (err) {
      setOsMessage(err instanceof ApiError ? err.message : 'Failed to clear')
    } finally {
      setOsSaving(false)
    }
  }

  async function handleToggleMusic(enabled: boolean) {
    setMusicSaving(true)
    try {
      setSettings(await updateIntegrationSettings({ musicMetadataEnabled: enabled }))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save')
    } finally {
      setMusicSaving(false)
    }
  }

  async function handleToggleAudiobook(enabled: boolean) {
    setAudiobookSaving(true)
    try {
      setSettings(await updateIntegrationSettings({ audiobookMetadataEnabled: enabled }))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save')
    } finally {
      setAudiobookSaving(false)
    }
  }

  if (error) return <p className="vorn-form-error">{error}</p>
  if (!settings) return <p>Loading…</p>

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Integrations</h1>
        <p className="vorn-admin-page-subtitle">
          API credentials for external services. These override the equivalent VORN_* environment variables when
          set, and only take effect after restarting the server.
        </p>
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>TMDb</h2>
          <StatusBadge configured={settings.tmdbConfigured} />
        </div>
        <p className="vorn-panel-subtitle">Powers metadata and artwork (posters/backdrops) matching during library sync.</p>
        <form className="vorn-inline-form" onSubmit={handleSaveTmdb}>
          <input
            type="password"
            placeholder={settings.tmdbConfigured ? '•••••••• (unchanged)' : 'TMDb API key'}
            value={tmdbKeyInput}
            onChange={(e) => setTmdbKeyInput(e.target.value)}
            style={{ minWidth: '20rem' }}
          />
          <button type="submit" disabled={tmdbSaving || tmdbKeyInput.trim() === ''}>
            {tmdbSaving ? 'Saving…' : 'Save'}
          </button>
          {settings.tmdbConfigured && (
            <button type="button" className="vorn-btn-danger" onClick={handleClearTmdb} disabled={tmdbSaving}>
              Clear
            </button>
          )}
        </form>
        {tmdbMessage && <p>{tmdbMessage}</p>}
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>OpenSubtitles</h2>
          <StatusBadge configured={settings.openSubtitlesConfigured} />
        </div>
        <p className="vorn-panel-subtitle">Powers subtitle search and download from the watch page.</p>
        <form
          className="vorn-inline-form"
          onSubmit={handleSaveOpenSubtitles}
          style={{ flexDirection: 'column', alignItems: 'flex-start', gap: '0.6rem' }}
        >
          <label>
            API key{' '}
            <input
              type="password"
              placeholder={settings.openSubtitlesConfigured ? '•••••••• (unchanged)' : 'OpenSubtitles API key'}
              value={osKeyInput}
              onChange={(e) => setOsKeyInput(e.target.value)}
              style={{ minWidth: '20rem' }}
            />
          </label>
          <label>
            Username{' '}
            <input
              value={osUsername}
              onChange={(e) => setOsUsername(e.target.value)}
              placeholder="OpenSubtitles username"
              style={{ minWidth: '20rem' }}
            />
          </label>
          <label>
            Password{' '}
            <input
              type="password"
              placeholder={settings.openSubtitlesConfigured ? '•••••••• (unchanged)' : 'OpenSubtitles password'}
              value={osPasswordInput}
              onChange={(e) => setOsPasswordInput(e.target.value)}
              style={{ minWidth: '20rem' }}
            />
          </label>
          <div className="vorn-button-group">
            <button type="submit" disabled={osSaving}>
              {osSaving ? 'Saving…' : 'Save'}
            </button>
            {settings.openSubtitlesConfigured && (
              <button type="button" className="vorn-btn-danger" onClick={handleClearOpenSubtitles} disabled={osSaving}>
                Clear
              </button>
            )}
          </div>
        </form>
        {osMessage && <p>{osMessage}</p>}
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Music metadata</h2>
          <span className={`vorn-status-badge ${settings.musicMetadataEnabled ? 'vorn-status-badge-on' : 'vorn-status-badge-off'}`}>
            {settings.musicMetadataEnabled ? 'Enabled' : 'Disabled'}
          </span>
        </div>
        <p className="vorn-panel-subtitle">
          Matches albums against MusicBrainz and fetches cover art from Cover Art Archive. Free, no API key or
          account needed — this toggle exists because it's still an outbound call to a third party, and that should
          be your choice, not the default. Takes effect after restarting the server.
        </p>
        <label>
          <input
            type="checkbox"
            checked={settings.musicMetadataEnabled}
            disabled={musicSaving}
            onChange={(e) => handleToggleMusic(e.target.checked)}
          />{' '}
          Enable MusicBrainz + Cover Art Archive
        </label>
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Audiobook metadata</h2>
          <span
            className={`vorn-status-badge ${settings.audiobookMetadataEnabled ? 'vorn-status-badge-on' : 'vorn-status-badge-off'}`}
          >
            {settings.audiobookMetadataEnabled ? 'Enabled' : 'Disabled'}
          </span>
        </div>
        <p className="vorn-panel-subtitle">
          Matches books against Open Library and fetches cover art from its covers API. Free, no API key or account
          needed (Audible has no public API). Takes effect after restarting the server.
        </p>
        <label>
          <input
            type="checkbox"
            checked={settings.audiobookMetadataEnabled}
            disabled={audiobookSaving}
            onChange={(e) => handleToggleAudiobook(e.target.checked)}
          />{' '}
          Enable Open Library
        </label>
      </div>
    </section>
  )
}

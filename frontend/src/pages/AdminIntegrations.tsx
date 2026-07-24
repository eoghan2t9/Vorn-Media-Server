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

  const [fanartKeyInput, setFanartKeyInput] = useState('')
  const [fanartSaving, setFanartSaving] = useState(false)
  const [fanartMessage, setFanartMessage] = useState<string | null>(null)

  const [omdbKeyInput, setOmdbKeyInput] = useState('')
  const [omdbSaving, setOmdbSaving] = useState(false)
  const [omdbMessage, setOmdbMessage] = useState<string | null>(null)

  const [tvdbKeyInput, setTvdbKeyInput] = useState('')
  const [tvdbPinInput, setTvdbPinInput] = useState('')
  const [tvdbSaving, setTvdbSaving] = useState(false)
  const [tvdbMessage, setTvdbMessage] = useState<string | null>(null)

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

  async function handleSaveFanart(e: FormEvent) {
    e.preventDefault()
    if (fanartKeyInput.trim() === '') return
    setFanartMessage(null)
    setFanartSaving(true)
    try {
      const s = await updateIntegrationSettings({ fanartApiKey: fanartKeyInput.trim() })
      setSettings(s)
      setFanartKeyInput('')
      setFanartMessage('Saved. Restart the server for this to take effect.')
    } catch (err) {
      setFanartMessage(err instanceof ApiError ? err.message : 'Failed to save')
    } finally {
      setFanartSaving(false)
    }
  }

  async function handleClearFanart() {
    setFanartMessage(null)
    setFanartSaving(true)
    try {
      const s = await updateIntegrationSettings({ fanartApiKey: '' })
      setSettings(s)
      setFanartKeyInput('')
      setFanartMessage('Cleared. Restart the server for this to take effect.')
    } catch (err) {
      setFanartMessage(err instanceof ApiError ? err.message : 'Failed to clear')
    } finally {
      setFanartSaving(false)
    }
  }

  async function handleSaveOmdb(e: FormEvent) {
    e.preventDefault()
    if (omdbKeyInput.trim() === '') return
    setOmdbMessage(null)
    setOmdbSaving(true)
    try {
      const s = await updateIntegrationSettings({ omdbApiKey: omdbKeyInput.trim() })
      setSettings(s)
      setOmdbKeyInput('')
      setOmdbMessage('Saved. Restart the server for this to take effect.')
    } catch (err) {
      setOmdbMessage(err instanceof ApiError ? err.message : 'Failed to save')
    } finally {
      setOmdbSaving(false)
    }
  }

  async function handleClearOmdb() {
    setOmdbMessage(null)
    setOmdbSaving(true)
    try {
      const s = await updateIntegrationSettings({ omdbApiKey: '' })
      setSettings(s)
      setOmdbKeyInput('')
      setOmdbMessage('Cleared. Restart the server for this to take effect.')
    } catch (err) {
      setOmdbMessage(err instanceof ApiError ? err.message : 'Failed to clear')
    } finally {
      setOmdbSaving(false)
    }
  }

  async function handleSaveTvdb(e: FormEvent) {
    e.preventDefault()
    if (tvdbKeyInput.trim() === '') return
    setTvdbMessage(null)
    setTvdbSaving(true)
    try {
      const s = await updateIntegrationSettings({
        tvdbApiKey: tvdbKeyInput.trim(),
        tvdbPin: tvdbPinInput.trim() || undefined,
      })
      setSettings(s)
      setTvdbKeyInput('')
      setTvdbPinInput('')
      setTvdbMessage('Saved. Restart the server for this to take effect.')
    } catch (err) {
      setTvdbMessage(err instanceof ApiError ? err.message : 'Failed to save')
    } finally {
      setTvdbSaving(false)
    }
  }

  async function handleClearTvdb() {
    setTvdbMessage(null)
    setTvdbSaving(true)
    try {
      const s = await updateIntegrationSettings({ tvdbApiKey: '', tvdbPin: '' })
      setSettings(s)
      setTvdbKeyInput('')
      setTvdbPinInput('')
      setTvdbMessage('Cleared. Restart the server for this to take effect.')
    } catch (err) {
      setTvdbMessage(err instanceof ApiError ? err.message : 'Failed to clear')
    } finally {
      setTvdbSaving(false)
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
          <h2>TheTVDB (fallback series matching)</h2>
          <StatusBadge configured={settings.tvdbConfigured} />
        </div>
        <p className="vorn-panel-subtitle">
          Tried only when TMDb has no match for a series. The Pin field is only needed for a "user-support" API key
          tied to an individual paid subscriber account — a standard project key leaves it blank.
        </p>
        <form
          className="vorn-inline-form"
          onSubmit={handleSaveTvdb}
          style={{ flexDirection: 'column', alignItems: 'flex-start', gap: '0.6rem' }}
        >
          <label>
            API key{' '}
            <input
              type="password"
              placeholder={settings.tvdbConfigured ? '•••••••• (unchanged)' : 'TheTVDB API key'}
              value={tvdbKeyInput}
              onChange={(e) => setTvdbKeyInput(e.target.value)}
              style={{ minWidth: '20rem' }}
            />
          </label>
          <label>
            Pin (optional){' '}
            <input
              type="password"
              placeholder="Subscriber pin"
              value={tvdbPinInput}
              onChange={(e) => setTvdbPinInput(e.target.value)}
              style={{ minWidth: '20rem' }}
            />
          </label>
          <div className="vorn-button-group">
            <button type="submit" disabled={tvdbSaving || tvdbKeyInput.trim() === ''}>
              {tvdbSaving ? 'Saving…' : 'Save'}
            </button>
            {settings.tvdbConfigured && (
              <button type="button" className="vorn-btn-danger" onClick={handleClearTvdb} disabled={tvdbSaving}>
                Clear
              </button>
            )}
          </div>
        </form>
        {tvdbMessage && <p>{tvdbMessage}</p>}
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Fanart.tv (artwork)</h2>
          <StatusBadge configured={settings.fanartConfigured} />
        </div>
        <p className="vorn-panel-subtitle">
          Higher-resolution posters/backdrops and clear logos, layered on top of a TMDb (or TheTVDB fallback) match
          during library sync. Free API key.
        </p>
        <form className="vorn-inline-form" onSubmit={handleSaveFanart}>
          <input
            type="password"
            placeholder={settings.fanartConfigured ? '•••••••• (unchanged)' : 'Fanart.tv API key'}
            value={fanartKeyInput}
            onChange={(e) => setFanartKeyInput(e.target.value)}
            style={{ minWidth: '20rem' }}
          />
          <button type="submit" disabled={fanartSaving || fanartKeyInput.trim() === ''}>
            {fanartSaving ? 'Saving…' : 'Save'}
          </button>
          {settings.fanartConfigured && (
            <button type="button" className="vorn-btn-danger" onClick={handleClearFanart} disabled={fanartSaving}>
              Clear
            </button>
          )}
        </form>
        {fanartMessage && <p>{fanartMessage}</p>}
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>OMDb (ratings)</h2>
          <StatusBadge configured={settings.omdbConfigured} />
        </div>
        <p className="vorn-panel-subtitle">
          IMDb and Rotten Tomatoes ratings, layered onto a movie/series match that has an IMDb ID. Free tier is
          rate-limited to 1,000 requests/day.
        </p>
        <form className="vorn-inline-form" onSubmit={handleSaveOmdb}>
          <input
            type="password"
            placeholder={settings.omdbConfigured ? '•••••••• (unchanged)' : 'OMDb API key'}
            value={omdbKeyInput}
            onChange={(e) => setOmdbKeyInput(e.target.value)}
            style={{ minWidth: '20rem' }}
          />
          <button type="submit" disabled={omdbSaving || omdbKeyInput.trim() === ''}>
            {omdbSaving ? 'Saving…' : 'Save'}
          </button>
          {settings.omdbConfigured && (
            <button type="button" className="vorn-btn-danger" onClick={handleClearOmdb} disabled={omdbSaving}>
              Clear
            </button>
          )}
        </form>
        {omdbMessage && <p>{omdbMessage}</p>}
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
          be your choice, not the default. Takes effect immediately, on the next library metadata sync — no restart
          needed.
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
          needed (Audible has no public API). Takes effect immediately, on the next library metadata sync — no
          restart needed.
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

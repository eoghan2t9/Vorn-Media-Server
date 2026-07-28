import { useState, type FormEvent } from 'react'
import {
  ApiError,
  createDebridAccount,
  createLibrary,
  createNZBIndexer,
  createTorrentIndexer,
  createUsenetServer,
  initSetup,
  updateIntegrationSettings,
  type DebridAccount,
  type DebridProvider,
  type Library,
  type LibraryType,
  type NZBIndexer,
  type TorrentIndexer,
  type UsenetServer,
} from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { DirectoryBrowser } from '../components/DirectoryBrowser'
import { Select } from '../components/Select'
import { FolderIcon } from '../components/icons'
import './forms.css'
import './SetupWizard.css'

type StepId = 'admin' | 'library' | 'metadata' | 'torrent' | 'usenet' | 'debrid' | 'finish'

const STEPS: { id: StepId; label: string }[] = [
  { id: 'admin', label: 'Admin account' },
  { id: 'library', label: 'Library' },
  { id: 'metadata', label: 'Metadata' },
  { id: 'torrent', label: 'Torrents' },
  { id: 'usenet', label: 'Usenet' },
  { id: 'debrid', label: 'Debrid' },
  { id: 'finish', label: 'Finish' },
]

const TORRENT_INDEXER_PRESETS: { label: string; name: string; baseUrl: string }[] = [
  { label: 'Prowlarr', name: 'Prowlarr', baseUrl: 'http://localhost:9696/<indexer-id>' },
]

const NZB_INDEXER_PRESETS: { label: string; name: string; baseUrl: string }[] = [
  { label: 'NZBGeek', name: 'NZBGeek', baseUrl: 'https://api.nzbgeek.info' },
  { label: 'NZBFinder', name: 'NZBFinder', baseUrl: 'https://nzbfinder.ws' },
  { label: 'NZBPlanet', name: 'NZBPlanet', baseUrl: 'https://nzbplanet.net' },
  { label: 'DrunkenSlug', name: 'DrunkenSlug', baseUrl: 'https://drunkenslug.com' },
  { label: 'DOGnzb', name: 'DOGnzb', baseUrl: 'https://api.dognzb.cr' },
  { label: 'omgwtfnzbs', name: 'omgwtfnzbs', baseUrl: 'https://omgwtfnzbs.org' },
  { label: 'Tabula Rasa', name: 'Tabula Rasa', baseUrl: 'https://tabula-rasa.pw' },
]

const DEBRID_PROVIDERS: { value: DebridProvider; label: string }[] = [
  { value: 'realdebrid', label: 'Real-Debrid' },
  { value: 'torbox', label: 'TorBox' },
  { value: 'alldebrid', label: 'AllDebrid' },
  { value: 'premiumize', label: 'Premiumize' },
  { value: 'debridlink', label: 'Debrid-Link' },
]

export function SetupWizard() {
  const { login } = useAuth()

  const [stepIndex, setStepIndex] = useState(0)
  const step = STEPS[stepIndex]
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  // -- Admin account -----------------------------------------------------
  const [adminUsername, setAdminUsername] = useState('')
  const [adminPassword, setAdminPassword] = useState('')

  // -- Library -------------------------------------------------------------
  const [libraryName, setLibraryName] = useState('Movies')
  const [libraryType, setLibraryType] = useState<LibraryType>('movie')
  const [libraryPath, setLibraryPath] = useState('')
  const [libraryIs4K, setLibraryIs4K] = useState(false)
  const [browserOpen, setBrowserOpen] = useState(false)
  const [addedLibraries, setAddedLibraries] = useState<Library[]>([])

  // -- Metadata --------------------------------------------------------
  const [tmdbApiKey, setTmdbApiKey] = useState('')
  const [metadataSaved, setMetadataSaved] = useState(false)

  // -- Torrent indexers ------------------------------------------------
  const [torrentPreset, setTorrentPreset] = useState('')
  const [torrentName, setTorrentName] = useState('')
  const [torrentBaseUrl, setTorrentBaseUrl] = useState('')
  const [torrentApiKey, setTorrentApiKey] = useState('')
  const [addedTorrentIndexers, setAddedTorrentIndexers] = useState<TorrentIndexer[]>([])

  // -- Usenet: server + indexer -----------------------------------------
  const [usenetName, setUsenetName] = useState('TorBox')
  const [usenetServerApiKey, setUsenetServerApiKey] = useState('')
  const [addedUsenetServers, setAddedUsenetServers] = useState<UsenetServer[]>([])

  const [nzbIndexerPreset, setNzbIndexerPreset] = useState('')
  const [nzbIndexerName, setNzbIndexerName] = useState('')
  const [nzbIndexerBaseUrl, setNzbIndexerBaseUrl] = useState('')
  const [nzbIndexerApiKey, setNzbIndexerApiKey] = useState('')
  const [addedNzbIndexers, setAddedNzbIndexers] = useState<NZBIndexer[]>([])

  // -- Debrid ------------------------------------------------------------
  const [debridProvider, setDebridProvider] = useState<DebridProvider>('realdebrid')
  const [debridApiKey, setDebridApiKey] = useState('')
  const [addedDebridAccounts, setAddedDebridAccounts] = useState<DebridAccount[]>([])

  function goNext() {
    setError(null)
    setStepIndex((i) => Math.min(i + 1, STEPS.length - 1))
  }
  function goBack() {
    setError(null)
    setStepIndex((i) => Math.max(i - 1, 1))
  }

  function stepHasAnyProgress(id: StepId): boolean {
    switch (id) {
      case 'library':
        return addedLibraries.length > 0
      case 'metadata':
        return metadataSaved
      case 'torrent':
        return addedTorrentIndexers.length > 0
      case 'usenet':
        return addedUsenetServers.length > 0 || addedNzbIndexers.length > 0
      case 'debrid':
        return addedDebridAccounts.length > 0
      default:
        return false
    }
  }

  async function handleCreateAdmin(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await initSetup({ adminUsername, adminPassword })
      await login(adminUsername, adminPassword)
      // Deliberately not calling refreshSetupStatus() here -- the backend
      // already considers setup "complete" the moment an admin exists, but
      // AuthContext only re-checks that on an explicit call, so staying
      // silent here keeps the rest of this wizard reachable instead of the
      // app redirecting to /login mid-flow. The Finish step calls it once
      // everything's done.
      goNext()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to create admin account')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleAddLibrary(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const lib = await createLibrary({
        name: libraryName,
        type: libraryType,
        is4K: libraryIs4K,
        folders: libraryPath ? [libraryPath] : [],
      })
      setAddedLibraries((list) => [...list, lib])
      setLibraryPath('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add library')
    }
  }

  async function handleSaveMetadata(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      await updateIntegrationSettings({ tmdbApiKey })
      setMetadataSaved(true)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save metadata settings')
    }
  }

  function handleTorrentPreset(id: string) {
    setTorrentPreset(id)
    const preset = TORRENT_INDEXER_PRESETS.find((p) => p.label === id)
    if (preset) {
      setTorrentName(preset.name)
      setTorrentBaseUrl(preset.baseUrl)
    }
  }

  async function handleAddTorrentIndexer(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const idx = await createTorrentIndexer({ name: torrentName, baseUrl: torrentBaseUrl, apiKey: torrentApiKey || undefined })
      setAddedTorrentIndexers((list) => [...list, idx])
      setTorrentPreset('')
      setTorrentName('')
      setTorrentBaseUrl('')
      setTorrentApiKey('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add torrent indexer')
    }
  }

  async function handleAddUsenetServer(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const server = await createUsenetServer({ name: usenetName, apiKey: usenetServerApiKey })
      setAddedUsenetServers((list) => [...list, server])
      setUsenetName('TorBox')
      setUsenetServerApiKey('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add usenet server')
    }
  }

  function handleNzbIndexerPreset(id: string) {
    setNzbIndexerPreset(id)
    const preset = NZB_INDEXER_PRESETS.find((p) => p.label === id)
    if (preset) {
      setNzbIndexerName(preset.name)
      setNzbIndexerBaseUrl(preset.baseUrl)
    }
  }

  async function handleAddNzbIndexer(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const idx = await createNZBIndexer({ name: nzbIndexerName, baseUrl: nzbIndexerBaseUrl, apiKey: nzbIndexerApiKey || undefined })
      setAddedNzbIndexers((list) => [...list, idx])
      setNzbIndexerPreset('')
      setNzbIndexerName('')
      setNzbIndexerBaseUrl('')
      setNzbIndexerApiKey('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add NZB indexer')
    }
  }

  async function handleAddDebridAccount(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const account = await createDebridAccount({ provider: debridProvider, apiKey: debridApiKey })
      setAddedDebridAccounts((list) => [...list, account])
      setDebridApiKey('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add debrid account')
    }
  }

  function handleFinish() {
    // A hard navigation, not react-router's navigate() -- calling
    // refreshSetupStatus() while still rendering the /setup route flips
    // setupCompleted while pathname is still "/setup", and that route's own
    // conditional (setupCompleted ? <Navigate to="/login"> : <SetupWizard>)
    // wins the race against an in-SPA navigate('/'), landing on /login
    // instead. A full reload sidesteps the race entirely -- it re-runs
    // AuthProvider's bootstrap from scratch, which already fetches both
    // setup status and the current session correctly.
    window.location.href = '/'
  }

  return (
    <div className="vorn-form-page">
      <div className="vorn-auth-brand vorn-setup-brand">
        <img src="/favicon.svg" alt="" width={64} height={64} />
        <span>Vorn</span>
      </div>

      <ol className="vorn-setup-steps">
        {STEPS.map((s, i) => (
          <li
            key={s.id}
            className={i === stepIndex ? 'vorn-setup-step-current' : i < stepIndex ? 'vorn-setup-step-done' : 'vorn-setup-step-upcoming'}
          >
            <span className="vorn-setup-step-dot">{i < stepIndex ? '✓' : i + 1}</span>
            <span className="vorn-setup-step-label">{s.label}</span>
          </li>
        ))}
      </ol>

      <div className="vorn-form vorn-setup-form">
        {step.id === 'admin' && (
          <form onSubmit={handleCreateAdmin}>
            <h1>Welcome to Vorn</h1>
            <p className="vorn-form-subtitle">Let's create your admin account to get started.</p>
            <label>
              Admin username
              <input value={adminUsername} onChange={(e) => setAdminUsername(e.target.value)} minLength={3} required autoFocus />
            </label>
            <label>
              Admin password
              <input
                type="password"
                value={adminPassword}
                onChange={(e) => setAdminPassword(e.target.value)}
                minLength={8}
                required
              />
            </label>
            {error && <p className="vorn-form-error">{error}</p>}
            <button type="submit" disabled={submitting}>
              {submitting ? 'Creating…' : 'Create admin account'}
            </button>
          </form>
        )}

        {step.id === 'library' && (
          <div>
            <h1>Add a library</h1>
            <p className="vorn-form-subtitle">
              Point Vorn at a folder of movies or series. You can add more, or skip this and add one later.
            </p>
            {addedLibraries.length > 0 && (
              <ul className="vorn-setup-added-list">
                {addedLibraries.map((l) => (
                  <li key={l.id}>
                    ✓ {l.name} ({l.type})
                  </li>
                ))}
              </ul>
            )}
            <form className="vorn-inline-form vorn-setup-inline-form" onSubmit={handleAddLibrary}>
              <input placeholder="Name" value={libraryName} onChange={(e) => setLibraryName(e.target.value)} required />
              <Select
                value={libraryType}
                onChange={(v) => {
                  setLibraryType(v as LibraryType)
                  if (v !== 'movie' && v !== 'series') setLibraryIs4K(false)
                }}
                options={[
                  { value: 'movie', label: 'Movies' },
                  { value: 'series', label: 'Series' },
                  { value: 'music', label: 'Music' },
                  { value: 'audiobook', label: 'Audiobooks' },
                ]}
              />
              <input
                placeholder="Folder path on the server (optional)"
                value={libraryPath}
                onChange={(e) => setLibraryPath(e.target.value)}
              />
              {(libraryType === 'movie' || libraryType === 'series') && (
                <label className="vorn-setup-checkbox-label">
                  <input type="checkbox" checked={libraryIs4K} onChange={(e) => setLibraryIs4K(e.target.checked)} /> 4K only
                </label>
              )}
              <button type="button" onClick={() => setBrowserOpen(true)}>
                <FolderIcon style={{ verticalAlign: '-0.15em', marginRight: '0.35rem' }} />
                Browse…
              </button>
              <button type="submit">Add library</button>
            </form>
            {error && <p className="vorn-form-error">{error}</p>}
          </div>
        )}

        {step.id === 'metadata' && (
          <div>
            <h1>Metadata</h1>
            <p className="vorn-form-subtitle">
              A TMDb API key powers movie/series metadata, artwork, browse, and on-demand acquisition. Free to get at
              themoviedb.org. You can add this later in Admin → Integrations.
            </p>
            <form onSubmit={handleSaveMetadata}>
              <label>
                TMDb API key
                <input value={tmdbApiKey} onChange={(e) => setTmdbApiKey(e.target.value)} placeholder="Optional" />
              </label>
              {error && <p className="vorn-form-error">{error}</p>}
              <button type="submit" disabled={!tmdbApiKey || metadataSaved}>
                {metadataSaved ? 'Saved ✓' : 'Save'}
              </button>
            </form>
          </div>
        )}

        {step.id === 'torrent' && (
          <div>
            <h1>Torrent indexers</h1>
            <p className="vorn-form-subtitle">
              Connect a Torznab-compatible indexer manager (Prowlarr, or any other tool that speaks the same protocol) so
              Vorn can search for releases. Skip this and add one later in Admin → Torrents if you don't have one running yet.
            </p>
            {addedTorrentIndexers.length > 0 && (
              <ul className="vorn-setup-added-list">
                {addedTorrentIndexers.map((idx) => (
                  <li key={idx.id}>✓ {idx.name}</li>
                ))}
              </ul>
            )}
            <form className="vorn-inline-form vorn-setup-inline-form" onSubmit={handleAddTorrentIndexer}>
              <Select
                value={torrentPreset}
                onChange={handleTorrentPreset}
                placeholder="Preset…"
                options={TORRENT_INDEXER_PRESETS.map((p) => ({ value: p.label, label: p.label }))}
              />
              <input placeholder="Name" value={torrentName} onChange={(e) => setTorrentName(e.target.value)} required />
              <input
                placeholder="Base URL"
                value={torrentBaseUrl}
                onChange={(e) => setTorrentBaseUrl(e.target.value)}
                style={{ minWidth: '16rem' }}
                required
              />
              <input placeholder="API key" value={torrentApiKey} onChange={(e) => setTorrentApiKey(e.target.value)} />
              <button type="submit">Add indexer</button>
            </form>
            {error && <p className="vorn-form-error">{error}</p>}
          </div>
        )}

        {step.id === 'usenet' && (
          <div>
            <h1>Usenet</h1>
            <p className="vorn-form-subtitle">
              A TorBox account (for caching/streaming Usenet releases) and an indexer (to search for releases) are
              separate. Add either, both, or skip and configure them later in Admin → NZB / Usenet.
            </p>

            <h2 className="vorn-setup-subheading">TorBox account</h2>
            {addedUsenetServers.length > 0 && (
              <ul className="vorn-setup-added-list">
                {addedUsenetServers.map((s) => (
                  <li key={s.id}>✓ {s.name}</li>
                ))}
              </ul>
            )}
            <form className="vorn-inline-form vorn-setup-inline-form" onSubmit={handleAddUsenetServer}>
              <input placeholder="Name" value={usenetName} onChange={(e) => setUsenetName(e.target.value)} required />
              <input
                placeholder="API key"
                value={usenetServerApiKey}
                onChange={(e) => setUsenetServerApiKey(e.target.value)}
                required
              />
              <button type="submit">Add server</button>
            </form>

            <h2 className="vorn-setup-subheading">Indexer</h2>
            {addedNzbIndexers.length > 0 && (
              <ul className="vorn-setup-added-list">
                {addedNzbIndexers.map((idx) => (
                  <li key={idx.id}>✓ {idx.name}</li>
                ))}
              </ul>
            )}
            <form className="vorn-inline-form vorn-setup-inline-form" onSubmit={handleAddNzbIndexer}>
              <Select
                value={nzbIndexerPreset}
                onChange={handleNzbIndexerPreset}
                placeholder="Preset…"
                options={NZB_INDEXER_PRESETS.map((p) => ({ value: p.label, label: p.label }))}
              />
              <input placeholder="Name" value={nzbIndexerName} onChange={(e) => setNzbIndexerName(e.target.value)} required />
              <input
                placeholder="Base URL"
                value={nzbIndexerBaseUrl}
                onChange={(e) => setNzbIndexerBaseUrl(e.target.value)}
                style={{ minWidth: '16rem' }}
                required
              />
              <input placeholder="API key" value={nzbIndexerApiKey} onChange={(e) => setNzbIndexerApiKey(e.target.value)} />
              <button type="submit">Add indexer</button>
            </form>
            {error && <p className="vorn-form-error">{error}</p>}
          </div>
        )}

        {step.id === 'debrid' && (
          <div>
            <h1>Debrid</h1>
            <p className="vorn-form-subtitle">
              Add a debrid account (Real-Debrid, TorBox, AllDebrid, Premiumize, Debrid-Link) for on-demand acquisition and
              direct streaming. Skip this and add one later in Admin → Debrid.
            </p>
            {addedDebridAccounts.length > 0 && (
              <ul className="vorn-setup-added-list">
                {addedDebridAccounts.map((a) => (
                  <li key={a.id}>✓ {DEBRID_PROVIDERS.find((p) => p.value === a.provider)?.label ?? a.provider}</li>
                ))}
              </ul>
            )}
            <form className="vorn-inline-form vorn-setup-inline-form" onSubmit={handleAddDebridAccount}>
              <Select
                value={debridProvider}
                onChange={(v) => setDebridProvider(v as DebridProvider)}
                options={DEBRID_PROVIDERS.map((p) => ({ value: p.value, label: p.label }))}
              />
              <input placeholder="API key" value={debridApiKey} onChange={(e) => setDebridApiKey(e.target.value)} required />
              <button type="submit">Add account</button>
            </form>
            {error && <p className="vorn-form-error">{error}</p>}
          </div>
        )}

        {step.id === 'finish' && (
          <div>
            <h1>All set</h1>
            <p className="vorn-form-subtitle">Here's what's configured. You can change any of this later from the Admin area.</p>
            <ul className="vorn-setup-summary">
              <li>Admin account: {adminUsername}</li>
              <li>Libraries: {addedLibraries.length || 'none yet'}</li>
              <li>TMDb metadata: {metadataSaved ? 'configured' : 'not configured'}</li>
              <li>Torrent indexers: {addedTorrentIndexers.length || 'none yet'}</li>
              <li>Usenet servers: {addedUsenetServers.length || 'none yet'}</li>
              <li>Usenet indexers: {addedNzbIndexers.length || 'none yet'}</li>
              <li>Debrid accounts: {addedDebridAccounts.length || 'none yet'}</li>
            </ul>
            <button type="button" onClick={handleFinish}>
              Finish setup
            </button>
          </div>
        )}

        {step.id !== 'admin' && step.id !== 'finish' && (
          <div className="vorn-setup-nav">
            <button type="button" className="vorn-setup-back" onClick={goBack} disabled={stepIndex <= 1}>
              Back
            </button>
            <button type="button" onClick={goNext}>
              {stepHasAnyProgress(step.id) ? 'Continue' : 'Skip for now'}
            </button>
          </div>
        )}
      </div>

      {browserOpen && (
        <DirectoryBrowser
          initialPath={libraryPath || undefined}
          onClose={() => setBrowserOpen(false)}
          onSelect={(path) => {
            setLibraryPath(path)
            setBrowserOpen(false)
          }}
        />
      )}
    </div>
  )
}

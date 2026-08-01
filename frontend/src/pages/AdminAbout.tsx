import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, fetchHealth, listUpgrades, type AcquisitionUpgrade, type HealthResponse } from '../api/client'
import './AdminUsers.css'
import './AdminAbout.css'

interface CreditEntry {
  name: string
  // domain drives both the favicon lookup and, unless url is set for a
  // service with no dedicated site of its own (e.g. a GitHub-hosted tool),
  // the link itself.
  domain: string
  url?: string
  description: string
}

interface CreditGroup {
  title: string
  entries: CreditEntry[]
}

// Every external API/service Vorn knows how to talk to, sourced from the
// actual client implementations (internal/metadata, internal/subtitles,
// internal/debrid) and the indexer/provider presets on the Torrents/NZB
// admin pages -- not a generic "powered by" list, this is meant to stay in
// sync with what the codebase actually integrates.
const CREDIT_GROUPS: CreditGroup[] = [
  {
    title: 'Metadata & subtitles',
    entries: [
      { name: 'TMDb', domain: 'themoviedb.org', description: 'Movie and TV metadata, artwork, and search.' },
      { name: 'TheTVDB', domain: 'thetvdb.com', description: 'Series and episode metadata.' },
      { name: 'OMDb API', domain: 'omdbapi.com', description: 'Supplementary movie and series metadata.' },
      { name: 'Fanart.tv', domain: 'fanart.tv', description: 'High-resolution posters, backdrops, and logos.' },
      { name: 'MusicBrainz', domain: 'musicbrainz.org', description: 'Music metadata.' },
      { name: 'Cover Art Archive', domain: 'coverartarchive.org', description: 'Album cover art.' },
      { name: 'Open Library', domain: 'openlibrary.org', description: 'Audiobook and book metadata and covers.' },
      { name: 'OpenSubtitles', domain: 'opensubtitles.com', description: 'Subtitle search and download.' },
    ],
  },
  {
    title: 'Torrent indexing',
    entries: [{ name: 'Prowlarr', domain: 'prowlarr.com', description: 'Indexer manager (Torznab/Newznab).' }],
  },
  {
    title: 'Usenet indexers',
    entries: [
      { name: 'NZBGeek', domain: 'nzbgeek.info', description: 'Newznab-compatible Usenet indexer.' },
      { name: 'NZBFinder', domain: 'nzbfinder.ws', description: 'Newznab-compatible Usenet indexer.' },
      { name: 'NZBPlanet', domain: 'nzbplanet.net', description: 'Newznab-compatible Usenet indexer.' },
      { name: 'DrunkenSlug', domain: 'drunkenslug.com', description: 'Newznab-compatible Usenet indexer.' },
      { name: 'DOGnzb', domain: 'dognzb.cr', description: 'Newznab-compatible Usenet indexer.' },
      { name: 'omgwtfnzbs', domain: 'omgwtfnzbs.org', description: 'Newznab-compatible Usenet indexer.' },
      { name: 'Tabula Rasa', domain: 'tabula-rasa.pw', description: 'Newznab-compatible Usenet indexer.' },
    ],
  },
  {
    title: 'Usenet providers',
    entries: [
      { name: 'Newshosting', domain: 'newshosting.com', description: 'Usenet server access.' },
      { name: 'Eweka', domain: 'eweka.nl', description: 'Usenet server access.' },
      { name: 'UsenetServer', domain: 'usenetserver.com', description: 'Usenet server access.' },
      { name: 'Tweaknews', domain: 'tweaknews.eu', description: 'Usenet server access.' },
      { name: 'Easynews', domain: 'easynews.com', description: 'Usenet server access.' },
      { name: 'Astraweb', domain: 'astraweb.com', description: 'Usenet server access.' },
      { name: 'Giganews', domain: 'giganews.com', description: 'Usenet server access.' },
      { name: 'XS News', domain: 'xsnews.nl', description: 'Usenet server access.' },
    ],
  },
  {
    title: 'Debrid',
    entries: [
      { name: 'Real-Debrid', domain: 'real-debrid.com', description: 'Debrid resolve and direct streaming.' },
      { name: 'TorBox', domain: 'torbox.app', description: 'Debrid resolve and direct streaming.' },
      { name: 'AllDebrid', domain: 'alldebrid.com', description: 'Debrid resolve and direct streaming.' },
      { name: 'Premiumize', domain: 'premiumize.me', description: 'Debrid resolve and direct streaming.' },
      { name: 'Debrid-Link', domain: 'debrid-link.com', description: 'Debrid resolve and direct streaming.' },
    ],
  },
]

function ProviderLogo({ domain, name }: { domain: string; name: string }) {
  const [failed, setFailed] = useState(false)
  if (failed) {
    return (
      <div className="vorn-credit-logo-fallback" aria-hidden="true">
        {name.charAt(0)}
      </div>
    )
  }
  return (
    <img
      className="vorn-credit-logo"
      src={`https://www.google.com/s2/favicons?sz=64&domain=${domain}`}
      alt=""
      loading="lazy"
      onError={() => setFailed(true)}
    />
  )
}

export function AdminAbout() {
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [upgrades, setUpgrades] = useState<AcquisitionUpgrade[]>([])
  const [upgradesErr, setUpgradesErr] = useState<string | null>(null)

  useEffect(() => {
    fetchHealth()
      .then(setHealth)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
    listUpgrades()
      .then(setUpgrades)
      .catch((err) => setUpgradesErr(err instanceof ApiError ? err.message : String(err)))
  }, [])

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>About</h1>
        <p className="vorn-admin-page-subtitle">System version and the external services Vorn integrates with.</p>
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>System</h2>
        </div>
        {error && <p className="vorn-form-error">{error}</p>}
        <div className="vorn-table-wrap">
          <table className="vorn-table">
            <tbody>
              <tr>
                <th>Version</th>
                <td>{health?.version ?? '—'}</td>
              </tr>
              <tr>
                <th>Status</th>
                <td>{health?.status ?? '—'}</td>
              </tr>
              <tr>
                <th>Uptime</th>
                <td>{health?.uptime ?? '—'}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p className="vorn-panel-subtitle" style={{ margin: '0.75rem 0 0' }}>
          To check for or apply an update, see <Link to="/admin/server-settings">Network</Link>.
        </p>
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Quality Upgrades</h2>
        </div>
        {upgradesErr && <p className="vorn-form-error">{upgradesErr}</p>}
        {upgrades.length === 0 ? (
          <p className="vorn-empty">No quality upgrades recorded yet. When the monitor finds a better release for a monitored item, it'll show up here.</p>
        ) : (
          <div className="vorn-table-wrap">
          <table className="vorn-table">
            <thead>
              <tr>
                <th>Title</th>
                <th>Old Release</th>
                <th>New Release</th>
                <th>Source</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {upgrades.map((u) => (
                <tr key={u.id}>
                  <td><Link to={`/items/${u.itemId}`}>{u.title}</Link></td>
                  <td className="vorn-muted">{u.oldRelease || '—'}</td>
                  <td>{u.newRelease}</td>
                  <td>{u.source}</td>
                  <td className="vorn-muted">{new Date(u.createdAt).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
        )}
      </div>

      {CREDIT_GROUPS.map((group) => (
        <div className="vorn-panel" key={group.title}>
          <div className="vorn-panel-header">
            <h2>{group.title}</h2>
          </div>
          <div className="vorn-credit-grid">
            {group.entries.map((entry) => (
              <a
                key={entry.name}
                className="vorn-credit-card"
                href={entry.url ?? `https://${entry.domain}`}
                target="_blank"
                rel="noreferrer noopener"
              >
                <ProviderLogo domain={entry.domain} name={entry.name} />
                <div className="vorn-credit-body">
                  <div className="vorn-credit-name">{entry.name}</div>
                  <div className="vorn-credit-desc">{entry.description}</div>
                </div>
              </a>
            ))}
          </div>
        </div>
      ))}
    </section>
  )
}

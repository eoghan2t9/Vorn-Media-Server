import { useEffect, useState, type FormEvent } from 'react'
import {
  ApiError,
  addDebridLink,
  createDebridAccount,
  deleteDebridAccount,
  listDebridAccounts,
  listDebridItems,
  listLibraries,
  removeDebridItem,
  type DebridAccount,
  type DebridItem,
  type Library,
} from '../api/client'
import { Select } from '../components/Select'
import './AdminUsers.css'

export function AdminDebrid() {
  const [items, setItems] = useState<DebridItem[]>([])
  const [libraries, setLibraries] = useState<Library[]>([])
  const [accounts, setAccounts] = useState<DebridAccount[]>([])
  const [error, setError] = useState<string | null>(null)

  const [accountId, setAccountId] = useState('')
  const [sourceRef, setSourceRef] = useState('')
  const [name, setName] = useState('')
  const [libraryId, setLibraryId] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const [newProvider, setNewProvider] = useState<'realdebrid' | 'torbox'>('realdebrid')
  const [newApiKey, setNewApiKey] = useState('')

  async function refreshItems() {
    setItems(await listDebridItems())
  }

  useEffect(() => {
    Promise.all([refreshItems(), listLibraries().then(setLibraries), listDebridAccounts().then(setAccounts)]).catch((err) =>
      setError(err instanceof ApiError ? err.message : String(err)),
    )
    const interval = setInterval(() => {
      refreshItems().catch(() => {})
    }, 2000)
    return () => clearInterval(interval)
  }, [])

  async function handleAddLink(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await addDebridLink({
        accountId,
        sourceRef,
        name: name || undefined,
        libraryId: libraryId || undefined,
      })
      setSourceRef('')
      setName('')
      await refreshItems()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add link')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleRemove(id: string) {
    try {
      await removeDebridItem(id)
      await refreshItems()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to remove item')
    }
  }

  async function handleAddAccount(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const account = await createDebridAccount({ provider: newProvider, apiKey: newApiKey })
      setAccounts((list) => [...list, account])
      setNewApiKey('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add account')
    }
  }

  async function handleDeleteAccount(id: string) {
    try {
      await deleteDebridAccount(id)
      setAccounts((list) => list.filter((a) => a.id !== id))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete account')
    }
  }

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Debrid</h1>
        <p className="vorn-admin-page-subtitle">Resolve magnets/hashes through a debrid provider instead of downloading torrents locally.</p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Resolved links</h2>
        </div>
        <div className="vorn-table-wrap">
        <table className="vorn-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>Promoted</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((it) => (
              <tr key={it.id}>
                <td>{it.name || it.sourceRef}</td>
                <td>{it.status === 'error' ? `error: ${it.error}` : it.status}</td>
                <td>{it.promoted ? 'yes' : 'no'}</td>
                <td>
                  <button type="button" className="vorn-btn-danger" onClick={() => handleRemove(it.id)}>
                    Remove
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Add magnet / hash</h2>
        </div>
        <form className="vorn-inline-form" onSubmit={handleAddLink}>
          <Select
            value={accountId}
            onChange={setAccountId}
            placeholder="Select account…"
            options={accounts.map((a) => ({
              value: a.id,
              label: `${a.provider}${!a.enabled ? ' (disabled)' : ''}`,
            }))}
          />
          <input
            placeholder="Magnet URI or info-hash"
            value={sourceRef}
            onChange={(e) => setSourceRef(e.target.value)}
            style={{ minWidth: '20rem' }}
            required
          />
          <input placeholder="Name (optional)" value={name} onChange={(e) => setName(e.target.value)} />
          <Select
            value={libraryId}
            onChange={setLibraryId}
            placeholder="No destination library"
            options={libraries.map((l) => ({ value: l.id, label: l.name }))}
          />
          <button type="submit" disabled={submitting || !accountId}>
            {submitting ? 'Adding…' : 'Resolve'}
          </button>
        </form>
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Debrid accounts</h2>
        </div>
        <div className="vorn-table-wrap">
        <table className="vorn-table">
          <thead>
            <tr>
              <th>Provider</th>
              <th>Enabled</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {accounts.map((a) => (
              <tr key={a.id}>
                <td>{a.provider}</td>
                <td>{a.enabled ? 'yes' : 'no'}</td>
                <td>
                  <button type="button" className="vorn-btn-danger" onClick={() => handleDeleteAccount(a.id)}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
        <form className="vorn-inline-form" onSubmit={handleAddAccount} style={{ marginTop: '1rem' }}>
          <Select
            value={newProvider}
            onChange={(v) => setNewProvider(v as 'realdebrid' | 'torbox')}
            options={[
              { value: 'realdebrid', label: 'Real-Debrid' },
              { value: 'torbox', label: 'TorBox' },
            ]}
          />
          <input
            placeholder="API key"
            type="password"
            value={newApiKey}
            onChange={(e) => setNewApiKey(e.target.value)}
            style={{ minWidth: '16rem' }}
            required
          />
          <button type="submit">Add account</button>
        </form>
      </div>
    </section>
  )
}

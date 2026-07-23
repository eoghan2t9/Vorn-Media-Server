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
    <section className="vorn-admin-users">
      <h1>Debrid</h1>
      {error && <p className="vorn-form-error">{error}</p>}

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
                <button type="button" onClick={() => handleRemove(it.id)}>
                  Remove
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <h2>Add magnet / hash</h2>
      <form className="vorn-inline-form" onSubmit={handleAddLink}>
        <select value={accountId} onChange={(e) => setAccountId(e.target.value)} required>
          <option value="">Select account…</option>
          {accounts.map((a) => (
            <option key={a.id} value={a.id}>
              {a.provider} {!a.enabled ? '(disabled)' : ''}
            </option>
          ))}
        </select>
        <input
          placeholder="Magnet URI or info-hash"
          value={sourceRef}
          onChange={(e) => setSourceRef(e.target.value)}
          style={{ minWidth: '20rem' }}
          required
        />
        <input placeholder="Name (optional)" value={name} onChange={(e) => setName(e.target.value)} />
        <select value={libraryId} onChange={(e) => setLibraryId(e.target.value)}>
          <option value="">No destination library (won't auto-add)</option>
          {libraries.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </select>
        <button type="submit" disabled={submitting || !accountId}>
          {submitting ? 'Adding…' : 'Resolve'}
        </button>
      </form>

      <h2>Debrid accounts</h2>
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
                <button type="button" onClick={() => handleDeleteAccount(a.id)}>
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <form className="vorn-inline-form" onSubmit={handleAddAccount}>
        <select value={newProvider} onChange={(e) => setNewProvider(e.target.value as 'realdebrid' | 'torbox')}>
          <option value="realdebrid">Real-Debrid</option>
          <option value="torbox">TorBox</option>
        </select>
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
    </section>
  )
}

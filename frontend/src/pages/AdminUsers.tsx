import { useEffect, useState, type FormEvent } from 'react'
import {
  ApiError,
  createUser,
  deleteUser,
  listLibraries,
  listUsers,
  setUserPermissions,
  type Library,
  type User,
} from '../api/client'
import './AdminUsers.css'

export function AdminUsers() {
  const [users, setUsers] = useState<User[]>([])
  const [libraries, setLibraries] = useState<Library[]>([])
  const [permissions, setPermissions] = useState<Record<string, string[]>>({})
  const [error, setError] = useState<string | null>(null)

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const [newUserLibraries, setNewUserLibraries] = useState<string[]>([])
  const [submitting, setSubmitting] = useState(false)

  async function refresh() {
    const [u, l] = await Promise.all([listUsers(), listLibraries()])
    setUsers(u)
    setLibraries(l)
  }

  useEffect(() => {
    refresh().catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await createUser({ username, password, isAdmin, libraryIds: newUserLibraries })
      setUsername('')
      setPassword('')
      setIsAdmin(false)
      setNewUserLibraries([])
      await refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to create user')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleDelete(id: string) {
    try {
      await deleteUser(id)
      await refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete user')
    }
  }

  async function handlePermissionsChange(userId: string, libraryIds: string[]) {
    setPermissions((p) => ({ ...p, [userId]: libraryIds }))
    try {
      await setUserPermissions(userId, libraryIds)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to update permissions')
    }
  }

  return (
    <section className="vorn-admin-users">
      <h1>Users</h1>
      {error && <p className="vorn-form-error">{error}</p>}

      <div className="vorn-table-wrap">
      <table className="vorn-table">
        <thead>
          <tr>
            <th>Username</th>
            <th>Role</th>
            <th>Library access</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {users.map((u) => (
            <tr key={u.id}>
              <td>{u.username}</td>
              <td>{u.isAdmin ? 'Admin (all libraries)' : 'Standard'}</td>
              <td>
                {!u.isAdmin && (
                  <select
                    multiple
                    value={permissions[u.id] ?? []}
                    onChange={(e) =>
                      handlePermissionsChange(
                        u.id,
                        Array.from(e.target.selectedOptions).map((o) => o.value),
                      )
                    }
                  >
                    {libraries.map((l) => (
                      <option key={l.id} value={l.id}>
                        {l.name}
                      </option>
                    ))}
                  </select>
                )}
              </td>
              <td>
                <button type="button" onClick={() => handleDelete(u.id)}>
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>

      <h2>Add user</h2>
      <form className="vorn-inline-form" onSubmit={handleCreate}>
        <input placeholder="Username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        <input
          type="password"
          placeholder="Password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          minLength={8}
        />
        <label>
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
          Admin
        </label>
        {!isAdmin && (
          <select
            multiple
            value={newUserLibraries}
            onChange={(e) => setNewUserLibraries(Array.from(e.target.selectedOptions).map((o) => o.value))}
          >
            {libraries.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </select>
        )}
        <button type="submit" disabled={submitting}>
          {submitting ? 'Adding…' : 'Add user'}
        </button>
      </form>
    </section>
  )
}

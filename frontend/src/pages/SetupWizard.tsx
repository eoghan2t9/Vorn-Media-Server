import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, initSetup } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import './forms.css'

export function SetupWizard() {
  const { refreshSetupStatus, login } = useAuth()
  const navigate = useNavigate()

  const [adminUsername, setAdminUsername] = useState('')
  const [adminPassword, setAdminPassword] = useState('')
  const [libraryName, setLibraryName] = useState('Movies')
  const [libraryType, setLibraryType] = useState<'movie' | 'series'>('movie')
  const [libraryPath, setLibraryPath] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await initSetup({
        adminUsername,
        adminPassword,
        libraryName: libraryPath ? libraryName : undefined,
        libraryType: libraryPath ? libraryType : undefined,
        libraryPath: libraryPath || undefined,
      })
      await refreshSetupStatus()
      await login(adminUsername, adminPassword)
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Setup failed')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="vorn-form-page">
      <form className="vorn-form" onSubmit={handleSubmit}>
        <h1>Welcome to Vorn</h1>
        <p className="vorn-form-subtitle">Let's create your admin account to get started.</p>

        <label>
          Admin username
          <input value={adminUsername} onChange={(e) => setAdminUsername(e.target.value)} minLength={3} required />
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

        <hr />
        <p className="vorn-form-subtitle">Optionally add your first library now (you can add more later).</p>

        <label>
          Library name
          <input value={libraryName} onChange={(e) => setLibraryName(e.target.value)} />
        </label>
        <label>
          Library type
          <select value={libraryType} onChange={(e) => setLibraryType(e.target.value as 'movie' | 'series')}>
            <option value="movie">Movies</option>
            <option value="series">Series</option>
          </select>
        </label>
        <label>
          Folder path (on the server)
          <input
            value={libraryPath}
            onChange={(e) => setLibraryPath(e.target.value)}
            placeholder="/media/movies"
          />
        </label>

        {error && <p className="vorn-form-error">{error}</p>}

        <button type="submit" disabled={submitting}>
          {submitting ? 'Setting up…' : 'Finish setup'}
        </button>
      </form>
    </div>
  )
}

import type { ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTheme } from '../theme/ThemeContext'
import { useAuth } from '../auth/AuthContext'
import { HeaderSearch } from './HeaderSearch'
import { HeaderStats } from './HeaderStats'
import './AppShell.css'

export function AppShell({ children }: { children: ReactNode }) {
  const { theme, toggleTheme } = useTheme()
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  async function handleLogout() {
    await logout()
    navigate('/login', { replace: true })
  }

  return (
    <div className="vorn-shell">
      <header className="vorn-header">
        <Link to="/" className="vorn-brand">
          <img src="/favicon.svg" alt="" width={28} height={28} />
          <span>Vorn</span>
        </Link>
        <nav className="vorn-nav">
          <Link to="/">Home</Link>
          {user?.isAdmin && <Link to="/admin">Admin</Link>}
        </nav>
        <HeaderSearch />
        {user?.isAdmin && <HeaderStats />}
        {user && (
          <span className="vorn-user">
            {user.username}
            {user.isAdmin && <span className="vorn-user-badge">admin</span>}
          </span>
        )}
        <button
          type="button"
          className="vorn-theme-toggle"
          onClick={toggleTheme}
          aria-label="Toggle color theme"
        >
          {theme === 'dark' ? '🌙' : '☀️'}
        </button>
        {user && (
          <button type="button" className="vorn-logout" onClick={handleLogout}>
            Sign out
          </button>
        )}
      </header>
      <main className="vorn-main">{children}</main>
    </div>
  )
}

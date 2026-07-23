import { useEffect, useState, type ReactNode } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useTheme } from '../theme/ThemeContext'
import { useAuth } from '../auth/AuthContext'
import { HeaderSearch } from './HeaderSearch'
import { HeaderStats } from './HeaderStats'
import './AppShell.css'

export function AppShell({ children }: { children: ReactNode }) {
  const { theme, toggleTheme } = useTheme()
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)

  // Close the mobile menu automatically on navigation, so it doesn't stay
  // open covering the new page.
  useEffect(() => {
    setMenuOpen(false)
  }, [location.pathname])

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

        <div className={`vorn-header-panel${menuOpen ? ' vorn-header-panel-open' : ''}`}>
          <nav className="vorn-nav">
            <NavLink to="/" end className={({ isActive }) => (isActive ? 'vorn-nav-active' : '')}>
              Home
            </NavLink>
            {user?.isAdmin && (
              <NavLink to="/admin" className={({ isActive }) => (isActive ? 'vorn-nav-active' : '')}>
                Admin
              </NavLink>
            )}
          </nav>
          <HeaderSearch />
          {user?.isAdmin && <HeaderStats />}
          {user && (
            <span className="vorn-user">
              {user.username}
              {user.isAdmin && <span className="vorn-user-badge">admin</span>}
            </span>
          )}
          {user && (
            <button type="button" className="vorn-logout" onClick={handleLogout}>
              Sign out
            </button>
          )}
        </div>

        <button
          type="button"
          className="vorn-theme-toggle"
          onClick={toggleTheme}
          aria-label="Toggle color theme"
        >
          {theme === 'dark' ? '🌙' : '☀️'}
        </button>
        <button
          type="button"
          className="vorn-menu-toggle"
          onClick={() => setMenuOpen((v) => !v)}
          aria-label="Toggle menu"
          aria-expanded={menuOpen}
        >
          {menuOpen ? '✕' : '☰'}
        </button>
      </header>
      <main className="vorn-main">{children}</main>
    </div>
  )
}

import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useTheme } from '../theme/ThemeContext'
import './AppShell.css'

export function AppShell({ children }: { children: ReactNode }) {
  const { theme, toggleTheme } = useTheme()

  return (
    <div className="vorn-shell">
      <header className="vorn-header">
        <Link to="/" className="vorn-brand">
          <img src="/favicon.svg" alt="" width={28} height={28} />
          <span>Vorn</span>
        </Link>
        <nav className="vorn-nav">
          <Link to="/">Home</Link>
          <Link to="/admin">Admin</Link>
        </nav>
        <button
          type="button"
          className="vorn-theme-toggle"
          onClick={toggleTheme}
          aria-label="Toggle color theme"
        >
          {theme === 'dark' ? '🌙' : '☀️'}
        </button>
      </header>
      <main className="vorn-main">{children}</main>
    </div>
  )
}

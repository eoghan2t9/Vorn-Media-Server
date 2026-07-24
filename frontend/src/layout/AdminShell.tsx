import { useEffect, useState, type ReactNode } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import {
  ArchiveIcon,
  CloudDownloadIcon,
  CloudIcon,
  DashboardIcon,
  EyeIcon,
  GlobeIcon,
  InboxIcon,
  LibraryIcon,
  MagnetIcon,
  MenuIcon,
  PlugIcon,
  TerminalIcon,
  UsersIcon,
} from '../components/icons'
import './AdminShell.css'

const NAV_GROUPS: { label: string; items: { to: string; label: string; icon: (props: { className?: string }) => ReactNode }[] }[] = [
  {
    label: 'Overview',
    items: [{ to: '/admin', label: 'Dashboard', icon: DashboardIcon }],
  },
  {
    label: 'Library',
    items: [
      { to: '/admin/libraries', label: 'Libraries', icon: LibraryIcon },
      { to: '/admin/users', label: 'Users', icon: UsersIcon },
      { to: '/admin/currently-watching', label: 'Currently watching', icon: EyeIcon },
      { to: '/admin/requests', label: 'Requests', icon: InboxIcon },
    ],
  },
  {
    label: 'Acquisition',
    items: [
      { to: '/admin/torrents', label: 'Torrents', icon: MagnetIcon },
      { to: '/admin/nzb', label: 'NZB / Usenet', icon: CloudDownloadIcon },
      { to: '/admin/debrid', label: 'Debrid', icon: CloudIcon },
    ],
  },
  {
    label: 'System',
    items: [
      { to: '/admin/integrations', label: 'Integrations', icon: PlugIcon },
      { to: '/admin/server-settings', label: 'Network', icon: GlobeIcon },
      { to: '/admin/backups', label: 'Backups', icon: ArchiveIcon },
      { to: '/admin/logs', label: 'Logs', icon: TerminalIcon },
    ],
  },
]

export function AdminShell({ children }: { children: ReactNode }) {
  const location = useLocation()
  const [sidebarOpen, setSidebarOpen] = useState(false)

  useEffect(() => {
    setSidebarOpen(false)
  }, [location.pathname])

  return (
    <div className="vorn-admin-shell">
      <button
        type="button"
        className="vorn-admin-sidebar-toggle"
        onClick={() => setSidebarOpen((v) => !v)}
        aria-label="Toggle admin menu"
        aria-expanded={sidebarOpen}
      >
        <MenuIcon />
        <span>Admin menu</span>
      </button>

      <aside className={`vorn-admin-sidebar${sidebarOpen ? ' vorn-admin-sidebar-open' : ''}`}>
        {NAV_GROUPS.map((group) => (
          <div className="vorn-admin-nav-group" key={group.label}>
            <div className="vorn-admin-nav-label">{group.label}</div>
            <nav>
              {group.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === '/admin'}
                  className={({ isActive }) => `vorn-admin-nav-link${isActive ? ' vorn-admin-nav-link-active' : ''}`}
                >
                  <item.icon className="vorn-admin-nav-icon" />
                  <span>{item.label}</span>
                </NavLink>
              ))}
            </nav>
          </div>
        ))}
      </aside>

      <div className="vorn-admin-content">{children}</div>
    </div>
  )
}

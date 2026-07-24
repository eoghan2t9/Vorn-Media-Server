import { Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { ThemeProvider } from './theme/ThemeContext'
import { AuthProvider, useAuth } from './auth/AuthContext'
import { AppShell } from './layout/AppShell'
import { AdminShell } from './layout/AdminShell'
import { ViewerHome } from './pages/ViewerHome'
import { Requests } from './pages/Requests'
import { AdminRequests } from './pages/AdminRequests'
import { AdminHome } from './pages/AdminHome'
import { AdminUsers } from './pages/AdminUsers'
import { AdminLibraries } from './pages/AdminLibraries'
import { ItemDetail } from './pages/ItemDetail'
import { WatchPage } from './pages/WatchPage'
import { AdminCurrentlyWatching } from './pages/AdminCurrentlyWatching'
import { AdminTorrents } from './pages/AdminTorrents'
import { AdminNzb } from './pages/AdminNzb'
import { AdminDebrid } from './pages/AdminDebrid'
import { AdminLogs } from './pages/AdminLogs'
import { AdminServerSettings } from './pages/AdminServerSettings'
import { AdminBackups } from './pages/AdminBackups'
import { AdminIntegrations } from './pages/AdminIntegrations'
import { SetupWizard } from './pages/SetupWizard'
import { Login } from './pages/Login'

function ConnectionError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="vorn-form-page">
      <div className="vorn-form">
        <h1>Can't reach Vorn</h1>
        <p className="vorn-form-error">{message}</p>
        <button type="button" onClick={onRetry}>
          Retry
        </button>
      </div>
    </div>
  )
}

function ProtectedLayout() {
  const { loading, setupCompleted, bootstrapError, user, retryBootstrap } = useAuth()

  if (loading) return null
  if (bootstrapError) return <ConnectionError message={bootstrapError} onRetry={retryBootstrap} />
  if (!setupCompleted) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace />
  return (
    <AppShell>
      <Outlet />
    </AppShell>
  )
}

function AdminLayout() {
  const { user } = useAuth()
  if (!user?.isAdmin) return <Navigate to="/" replace />
  return (
    <AdminShell>
      <Outlet />
    </AdminShell>
  )
}

function AppRoutes() {
  const { setupCompleted } = useAuth()

  return (
    <Routes>
      <Route path="/setup" element={setupCompleted ? <Navigate to="/login" replace /> : <SetupWizard />} />
      <Route path="/login" element={<Login />} />

      <Route element={<ProtectedLayout />}>
        <Route path="/" element={<ViewerHome />} />
        <Route path="/items/:id" element={<ItemDetail />} />
        <Route path="/watch/:id" element={<WatchPage />} />
        <Route path="/requests" element={<Requests />} />

        <Route element={<AdminLayout />}>
          <Route path="/admin" element={<AdminHome />} />
          <Route path="/admin/requests" element={<AdminRequests />} />
          <Route path="/admin/users" element={<AdminUsers />} />
          <Route path="/admin/libraries" element={<AdminLibraries />} />
          <Route path="/admin/currently-watching" element={<AdminCurrentlyWatching />} />
          <Route path="/admin/torrents" element={<AdminTorrents />} />
          <Route path="/admin/nzb" element={<AdminNzb />} />
          <Route path="/admin/debrid" element={<AdminDebrid />} />
          <Route path="/admin/logs" element={<AdminLogs />} />
          <Route path="/admin/server-settings" element={<AdminServerSettings />} />
          <Route path="/admin/backups" element={<AdminBackups />} />
          <Route path="/admin/integrations" element={<AdminIntegrations />} />
        </Route>
      </Route>

      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </ThemeProvider>
  )
}

export default App

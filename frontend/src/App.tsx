import { Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { ThemeProvider } from './theme/ThemeContext'
import { AuthProvider, useAuth } from './auth/AuthContext'
import { AppShell } from './layout/AppShell'
import { ViewerHome } from './pages/ViewerHome'
import { AdminHome } from './pages/AdminHome'
import { AdminUsers } from './pages/AdminUsers'
import { AdminLibraries } from './pages/AdminLibraries'
import { ItemDetail } from './pages/ItemDetail'
import { WatchPage } from './pages/WatchPage'
import { AdminCurrentlyWatching } from './pages/AdminCurrentlyWatching'
import { AdminTorrents } from './pages/AdminTorrents'
import { SetupWizard } from './pages/SetupWizard'
import { Login } from './pages/Login'

function ProtectedLayout() {
  const { loading, setupCompleted, user } = useAuth()

  if (loading) return null
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
  return <Outlet />
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

        <Route element={<AdminLayout />}>
          <Route path="/admin" element={<AdminHome />} />
          <Route path="/admin/users" element={<AdminUsers />} />
          <Route path="/admin/libraries" element={<AdminLibraries />} />
          <Route path="/admin/currently-watching" element={<AdminCurrentlyWatching />} />
          <Route path="/admin/torrents" element={<AdminTorrents />} />
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

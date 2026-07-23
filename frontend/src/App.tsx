import { Navigate, Route, Routes } from 'react-router-dom'
import { ThemeProvider } from './theme/ThemeContext'
import { AuthProvider, useAuth } from './auth/AuthContext'
import { AppShell } from './layout/AppShell'
import { ViewerHome } from './pages/ViewerHome'
import { AdminHome } from './pages/AdminHome'
import { AdminUsers } from './pages/AdminUsers'
import { SetupWizard } from './pages/SetupWizard'
import { Login } from './pages/Login'

function Gate({ children }: { children: React.ReactNode }) {
  const { loading, setupCompleted, user } = useAuth()

  if (loading) return null
  if (!setupCompleted) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace />
  return <>{children}</>
}

function AdminGate({ children }: { children: React.ReactNode }) {
  const { user } = useAuth()
  if (!user?.isAdmin) return <Navigate to="/" replace />
  return <>{children}</>
}

function AppRoutes() {
  const { setupCompleted } = useAuth()

  return (
    <Routes>
      <Route path="/setup" element={setupCompleted ? <Navigate to="/login" replace /> : <SetupWizard />} />
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <Gate>
            <AppShell>
              <ViewerHome />
            </AppShell>
          </Gate>
        }
      />
      <Route
        path="/admin"
        element={
          <Gate>
            <AdminGate>
              <AppShell>
                <AdminHome />
              </AppShell>
            </AdminGate>
          </Gate>
        }
      />
      <Route
        path="/admin/users"
        element={
          <Gate>
            <AdminGate>
              <AppShell>
                <AdminUsers />
              </AppShell>
            </AdminGate>
          </Gate>
        }
      />
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

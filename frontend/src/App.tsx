import { Navigate, Route, Routes } from 'react-router-dom'
import { ThemeProvider } from './theme/ThemeContext'
import { AppShell } from './layout/AppShell'
import { ViewerHome } from './pages/ViewerHome'
import { AdminHome } from './pages/AdminHome'

function App() {
  return (
    <ThemeProvider>
      <AppShell>
        <Routes>
          <Route path="/" element={<ViewerHome />} />
          <Route path="/admin" element={<AdminHome />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AppShell>
    </ThemeProvider>
  )
}

export default App

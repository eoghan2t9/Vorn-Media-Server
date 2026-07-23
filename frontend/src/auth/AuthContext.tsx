import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { ApiError, fetchMe, fetchSetupStatus, login as apiLogin, logout as apiLogout, type User } from '../api/client'

interface AuthContextValue {
  loading: boolean
  setupCompleted: boolean
  user: User | null
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refreshSetupStatus: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [setupCompleted, setSetupCompleted] = useState(false)
  const [user, setUser] = useState<User | null>(null)

  const refreshSetupStatus = useCallback(async () => {
    const status = await fetchSetupStatus()
    setSetupCompleted(status.completed)
  }, [])

  useEffect(() => {
    async function bootstrap() {
      try {
        const status = await fetchSetupStatus()
        setSetupCompleted(status.completed)
        if (status.completed) {
          try {
            setUser(await fetchMe())
          } catch (err) {
            if (!(err instanceof ApiError && err.status === 401)) throw err
          }
        }
      } finally {
        setLoading(false)
      }
    }
    bootstrap()
  }, [])

  const login = useCallback(async (username: string, password: string) => {
    const u = await apiLogin(username, password)
    setUser(u)
  }, [])

  const logout = useCallback(async () => {
    await apiLogout()
    setUser(null)
  }, [])

  return (
    <AuthContext.Provider value={{ loading, setupCompleted, user, login, logout, refreshSetupStatus }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider')
  return ctx
}

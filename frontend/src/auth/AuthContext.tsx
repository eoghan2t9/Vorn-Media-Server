import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { ApiError, fetchMe, fetchSetupStatus, login as apiLogin, logout as apiLogout, type User } from '../api/client'

interface AuthContextValue {
  loading: boolean
  setupCompleted: boolean | null
  bootstrapError: string | null
  user: User | null
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refreshSetupStatus: () => Promise<void>
  retryBootstrap: () => void
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [setupCompleted, setSetupCompleted] = useState<boolean | null>(null)
  const [bootstrapError, setBootstrapError] = useState<string | null>(null)
  const [user, setUser] = useState<User | null>(null)
  const [bootstrapAttempt, setBootstrapAttempt] = useState(0)

  const refreshSetupStatus = useCallback(async () => {
    const status = await fetchSetupStatus()
    setSetupCompleted(status.completed)
  }, [])

  const retryBootstrap = useCallback(() => {
    setLoading(true)
    setBootstrapError(null)
    setBootstrapAttempt((n) => n + 1)
  }, [])

  useEffect(() => {
    let cancelled = false
    async function bootstrap() {
      try {
        const status = await fetchSetupStatus()
        if (cancelled) return
        setSetupCompleted(status.completed)
        if (status.completed) {
          try {
            const u = await fetchMe()
            if (!cancelled) setUser(u)
          } catch (err) {
            if (!(err instanceof ApiError && err.status === 401)) throw err
          }
        }
      } catch (err) {
        // A failed bootstrap (network blip, CORS misconfig, backend restart)
        // must not be mistaken for "setup incomplete" -- that would strand an
        // already-set-up, already-logged-in user on the setup wizard. Surface
        // it as a retryable error instead of guessing at app state.
        if (!cancelled) setBootstrapError(err instanceof ApiError ? err.message : 'Could not reach the Vorn server')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    bootstrap()
    return () => {
      cancelled = true
    }
  }, [bootstrapAttempt])

  const login = useCallback(async (username: string, password: string) => {
    const u = await apiLogin(username, password)
    setUser(u)
  }, [])

  const logout = useCallback(async () => {
    await apiLogout()
    setUser(null)
  }, [])

  return (
    <AuthContext.Provider
      value={{ loading, setupCompleted, bootstrapError, user, login, logout, refreshSetupStatus, retryBootstrap }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider')
  return ctx
}

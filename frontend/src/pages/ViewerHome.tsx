import { useEffect, useState } from 'react'
import { fetchHealth, type HealthResponse } from '../api/client'

export function ViewerHome() {
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchHealth()
      .then(setHealth)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  return (
    <section>
      <h1>Welcome to Vorn</h1>
      <p>Your libraries will show up here once they're added in the admin area.</p>
      <p>
        Backend status:{' '}
        {health ? (
          <strong>{health.status} (v{health.version}, up {health.uptime})</strong>
        ) : error ? (
          <span>unreachable ({error})</span>
        ) : (
          'checking…'
        )}
      </p>
    </section>
  )
}

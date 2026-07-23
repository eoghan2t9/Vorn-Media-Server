import { useEffect, useState } from 'react'
import { fetchServerStats, type ServerStats } from '../api/client'

export function HeaderStats() {
  const [stats, setStats] = useState<ServerStats | null>(null)

  useEffect(() => {
    fetchServerStats().then(setStats).catch(() => setStats(null))
  }, [])

  if (!stats) return null

  return (
    <div className="vorn-header-stats" title="Server stats">
      <span>{stats.movieCount} movies</span>
      <span>{stats.seriesCount} series</span>
      <span>{stats.userCount} users</span>
    </div>
  )
}

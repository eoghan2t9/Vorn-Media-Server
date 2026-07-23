const API_BASE = import.meta.env.VITE_VORN_API_BASE ?? 'http://localhost:8080'

export interface HealthResponse {
  status: string
  uptime: string
  version: string
}

export async function fetchHealth(): Promise<HealthResponse> {
  const res = await fetch(`${API_BASE}/healthz`)
  if (!res.ok) throw new Error(`healthz returned ${res.status}`)
  return res.json()
}

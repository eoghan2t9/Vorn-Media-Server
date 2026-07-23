const API_BASE = import.meta.env.VITE_VORN_API_BASE ?? 'http://localhost:8080'

export interface HealthResponse {
  status: string
  uptime: string
  version: string
}

export interface User {
  id: string
  username: string
  isAdmin: boolean
  createdAt: string
}

export interface Library {
  id: string
  name: string
  type: 'movie' | 'series'
  folders: string[]
}

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
  })
  if (res.status === 204) return undefined as T
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new ApiError(res.status, body.error ?? `request failed with ${res.status}`)
  }
  return body as T
}

export const fetchHealth = () => request<HealthResponse>('/healthz')

export interface SetupStatus {
  completed: boolean
}
export const fetchSetupStatus = () => request<SetupStatus>('/api/setup/status')

export interface SetupInitInput {
  adminUsername: string
  adminPassword: string
  libraryName?: string
  libraryType?: 'movie' | 'series'
  libraryPath?: string
}
export const initSetup = (input: SetupInitInput) =>
  request<{ adminUsername: string; libraryId?: string }>('/api/setup/init', {
    method: 'POST',
    body: JSON.stringify(input),
  })

export const login = (username: string, password: string) =>
  request<User>('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) })

export const logout = () => request<void>('/api/auth/logout', { method: 'POST' })

export const fetchMe = () => request<User>('/api/auth/me')

export const listUsers = () => request<User[]>('/api/users')

export interface CreateUserInput {
  username: string
  password: string
  isAdmin: boolean
  libraryIds?: string[]
}
export const createUser = (input: CreateUserInput) =>
  request<User>('/api/users', { method: 'POST', body: JSON.stringify(input) })

export const updateUser = (id: string, input: { password?: string; isAdmin?: boolean }) =>
  request<User>(`/api/users/${id}`, { method: 'PATCH', body: JSON.stringify(input) })

export const deleteUser = (id: string) => request<void>(`/api/users/${id}`, { method: 'DELETE' })

export const setUserPermissions = (id: string, libraryIds: string[]) =>
  request<void>(`/api/users/${id}/permissions`, { method: 'PUT', body: JSON.stringify({ libraryIds }) })

export const listLibraries = () => request<Library[]>('/api/libraries')

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

export const getLibrary = (id: string) => request<Library>(`/api/libraries/${id}`)

export interface CreateLibraryInput {
  name: string
  type: 'movie' | 'series'
  folders: string[]
}
export const createLibrary = (input: CreateLibraryInput) =>
  request<Library>('/api/libraries', { method: 'POST', body: JSON.stringify(input) })

export const updateLibrary = (id: string, input: { name?: string; folders?: string[] }) =>
  request<Library>(`/api/libraries/${id}`, { method: 'PATCH', body: JSON.stringify(input) })

export const deleteLibrary = (id: string) => request<void>(`/api/libraries/${id}`, { method: 'DELETE' })

export interface ScanJob {
  id: string
  libraryId: string
  kind: 'real' | 'synthetic'
  status: 'running' | 'completed' | 'failed'
  filesFound: number
  filesSynced: number
  error?: string
  startedAt: string
  finishedAt?: string
}
export const startLibraryScan = (id: string) =>
  request<ScanJob>(`/api/libraries/${id}/scan`, { method: 'POST' })

export const listScanJobs = (libraryId?: string) =>
  request<ScanJob[]>(`/api/scan-jobs${libraryId ? `?libraryId=${libraryId}` : ''}`)

export const getScanJob = (id: string) => request<ScanJob>(`/api/scan-jobs/${id}`)

export interface MediaItem {
  id: string
  libraryId: string
  parentId?: string
  kind: 'movie' | 'series' | 'season' | 'episode'
  title: string
  overview?: string
  seasonNumber?: number
  episodeNumber?: number
  releaseDate?: string
  addedAt: string
}

export interface MediaItemDetail extends MediaItem {
  children?: MediaItem[]
}

export const listLibraryItems = (libraryId: string, opts?: { sort?: 'recent' | 'alpha'; kind?: string }) => {
  const params = new URLSearchParams()
  if (opts?.sort) params.set('sort', opts.sort)
  if (opts?.kind) params.set('kind', opts.kind)
  const qs = params.toString()
  return request<MediaItem[]>(`/api/libraries/${libraryId}/items${qs ? `?${qs}` : ''}`)
}

export const getItem = (id: string) => request<MediaItemDetail>(`/api/items/${id}`)

export const updateProgress = (id: string, positionSeconds: number, durationSeconds: number) =>
  request<void>(`/api/items/${id}/progress`, {
    method: 'PUT',
    body: JSON.stringify({ positionSeconds, durationSeconds }),
  })

export interface ContinueWatchingEntry {
  item: MediaItem
  positionSeconds: number
  durationSeconds: number
}
export const listContinueWatching = () => request<ContinueWatchingEntry[]>('/api/continue-watching')

export interface ServerStats {
  libraryCount: number
  userCount: number
  movieCount: number
  seriesCount: number
  episodeCount: number
  activeUsers: number
}
export const fetchServerStats = () => request<ServerStats>('/api/admin/stats')

export const search = (q: string) => request<MediaItem[]>(`/api/search?q=${encodeURIComponent(q)}`)

export interface MetadataJob {
  id: string
  libraryId: string
  status: 'running' | 'completed' | 'failed'
  itemsFound: number
  itemsMatched: number
  error?: string
  startedAt: string
  finishedAt?: string
}
export const startMetadataSync = (libraryId: string) =>
  request<MetadataJob>(`/api/libraries/${libraryId}/sync-metadata`, { method: 'POST' })

export const getMetadataJob = (id: string) => request<MetadataJob>(`/api/metadata-jobs/${id}`)

export interface UpdateMetadataInput {
  title?: string
  overview?: string
  releaseDate?: string
  tmdbId?: number
}
export const updateItemMetadata = (id: string, input: UpdateMetadataInput) =>
  request<MediaItem>(`/api/items/${id}/metadata`, { method: 'PATCH', body: JSON.stringify(input) })

export interface TranscodeCapabilities {
  backends: string[] | null
}
export const fetchTranscodeCapabilities = () => request<TranscodeCapabilities>('/api/transcode/capabilities')

export interface PlayResponse {
  mode: 'direct' | 'transcode'
  directUrl?: string
  sessionId?: string
  playlistUrl?: string
}
export const playItem = (id: string) => request<PlayResponse>(`/api/items/${id}/play`, { method: 'POST' })

export const stopStreamSession = (sessionId: string) =>
  request<void>(`/api/stream/session/${sessionId}`, { method: 'DELETE' })

export const getProgress = (id: string) =>
  request<{ positionSeconds: number; durationSeconds: number }>(`/api/items/${id}/progress`)

export interface CurrentlyWatchingEntry {
  username: string
  item: MediaItem
  positionSeconds: number
  durationSeconds: number
  updatedAt: string
}
export const listCurrentlyWatching = () => request<CurrentlyWatchingEntry[]>('/api/admin/currently-watching')

// API_BASE is exported so components that need an absolute stream URL
// (e.g. the HLS player, which hands the URL to hls.js/a <video> element
// rather than fetching it themselves) can build one.
export { API_BASE }

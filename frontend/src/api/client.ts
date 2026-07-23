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

export interface BrowseEntry {
  name: string
  path: string
}
export interface BrowseResult {
  path: string
  parent?: string
  directories: BrowseEntry[]
}
export const browseFilesystem = (path?: string) =>
  request<BrowseResult>(`/api/admin/browse${path ? `?path=${encodeURIComponent(path)}` : ''}`)

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
  posterUrl?: string
  backdropUrl?: string
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

// subtitlesUrl builds the URL for a WebVTT subtitle track suitable for a
// <track src="..."> element -- the endpoint fetches (and caches) on demand,
// so this can be pointed at directly rather than pre-fetched with `request`.
export const subtitlesUrl = (itemId: string, language: string) =>
  `${API_BASE}/api/items/${itemId}/subtitles?language=${encodeURIComponent(language)}`

export interface SubtitlesQuota {
  remaining: number
  resetTime?: string
}
export const fetchSubtitlesQuota = () => request<SubtitlesQuota>('/api/admin/subtitles/quota')

export interface ServerSettings {
  customDomain: string
  acmeEmail: string
  sslEnabled: boolean
  trustCloudflare: boolean
  updatedAt: string
}
export const fetchServerSettings = () => request<ServerSettings>('/api/admin/server-settings')

export interface UpdateServerSettingsInput {
  customDomain: string
  acmeEmail: string
  sslEnabled: boolean
  trustCloudflare: boolean
}
export const updateServerSettings = (input: UpdateServerSettingsInput) =>
  request<ServerSettings>('/api/admin/server-settings', { method: 'PUT', body: JSON.stringify(input) })

export interface IntegrationSettings {
  tmdbConfigured: boolean
  openSubtitlesConfigured: boolean
  openSubtitlesUsername?: string
  updatedAt: string
}
export const fetchIntegrationSettings = () => request<IntegrationSettings>('/api/admin/integrations')

// Fields left undefined are omitted from the request body, which the
// backend treats as "leave this credential unchanged" -- pass an empty
// string explicitly to clear one.
export interface UpdateIntegrationSettingsInput {
  tmdbApiKey?: string
  openSubtitlesApiKey?: string
  openSubtitlesUsername?: string
  openSubtitlesPassword?: string
}
export const updateIntegrationSettings = (input: UpdateIntegrationSettingsInput) =>
  request<IntegrationSettings>('/api/admin/integrations', { method: 'PUT', body: JSON.stringify(input) })

export interface UpdateCheckResult {
  currentVersion: string
  latestVersion?: string
  updateAvailable: boolean
  applied: boolean
  dockerized: boolean
}
export const checkForUpdate = () => request<UpdateCheckResult>('/api/admin/update/check')
export const applyUpdate = () => request<UpdateCheckResult>('/api/admin/update/apply', { method: 'POST' })

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

export interface Torrent {
  id: string
  libraryId?: string
  infoHash: string
  name: string
  sequential: boolean
  status: 'downloading' | 'seeding' | 'completed' | 'error' | 'removed'
  bytesTotal: number
  bytesDone: number
  error?: string
  addedAt: string
  completedAt?: string
}
export const listTorrents = () => request<Torrent[]>('/api/torrents')

export interface AddMagnetInput {
  magnetUri: string
  libraryId?: string
  sequential?: boolean
}
export const addMagnet = (input: AddMagnetInput) =>
  request<Torrent>('/api/torrents', { method: 'POST', body: JSON.stringify(input) })

export const addTorrentFile = async (file: File, opts?: { libraryId?: string; sequential?: boolean }) => {
  const params = new URLSearchParams()
  if (opts?.libraryId) params.set('libraryId', opts.libraryId)
  if (opts?.sequential) params.set('sequential', 'true')
  const qs = params.toString()
  const res = await fetch(`${API_BASE}/api/torrents/file${qs ? `?${qs}` : ''}`, {
    method: 'POST',
    credentials: 'include',
    body: await file.arrayBuffer(),
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, body.error ?? `request failed with ${res.status}`)
  return body as Torrent
}

export const removeTorrent = (id: string, deleteFiles = false) =>
  request<void>(`/api/torrents/${id}?deleteFiles=${deleteFiles}`, { method: 'DELETE' })

export interface TorrentSearchResult {
  indexerName: string
  title: string
  sizeBytes: number
  seeders: number
  peers: number
  downloadUrl: string
  publishedAt?: string
}
export const searchTorrents = (q: string) =>
  request<TorrentSearchResult[]>(`/api/torrents/search?q=${encodeURIComponent(q)}`)

export interface TorrentIndexer {
  id: string
  name: string
  baseUrl: string
  enabled: boolean
  createdAt: string
}
export const listTorrentIndexers = () => request<TorrentIndexer[]>('/api/torrent-indexers')

export const createTorrentIndexer = (input: { name: string; baseUrl: string; apiKey?: string }) =>
  request<TorrentIndexer>('/api/torrent-indexers', { method: 'POST', body: JSON.stringify(input) })

export const deleteTorrentIndexer = (id: string) =>
  request<void>(`/api/torrent-indexers/${id}`, { method: 'DELETE' })

export interface NZBDownload {
  id: string
  libraryId?: string
  name: string
  status: 'downloading' | 'repairing' | 'completed' | 'error' | 'removed'
  bytesTotal: number
  bytesDone: number
  error?: string
  promoted: boolean
  addedAt: string
  completedAt?: string
}
export const listNZBDownloads = () => request<NZBDownload[]>('/api/nzb')

export const addNZBFile = async (file: File, opts?: { libraryId?: string }) => {
  const params = new URLSearchParams()
  if (opts?.libraryId) params.set('libraryId', opts.libraryId)
  const qs = params.toString()
  const res = await fetch(`${API_BASE}/api/nzb${qs ? `?${qs}` : ''}`, {
    method: 'POST',
    credentials: 'include',
    body: await file.arrayBuffer(),
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, body.error ?? `request failed with ${res.status}`)
  return body as NZBDownload
}

export const removeNZBDownload = (id: string, deleteFiles = false) =>
  request<void>(`/api/nzb/${id}?deleteFiles=${deleteFiles}`, { method: 'DELETE' })

export interface UsenetServer {
  id: string
  name: string
  host: string
  port: number
  useTls: boolean
  username: string
  maxConnections: number
  enabled: boolean
  createdAt: string
}
export const listUsenetServers = () => request<UsenetServer[]>('/api/usenet-servers')

export interface CreateUsenetServerInput {
  name: string
  host: string
  port: number
  useTls?: boolean
  username?: string
  password?: string
  maxConnections?: number
}
export const createUsenetServer = (input: CreateUsenetServerInput) =>
  request<UsenetServer>('/api/usenet-servers', { method: 'POST', body: JSON.stringify(input) })

export const deleteUsenetServer = (id: string) =>
  request<void>(`/api/usenet-servers/${id}`, { method: 'DELETE' })

export interface DebridAccount {
  id: string
  provider: 'realdebrid' | 'torbox'
  enabled: boolean
  createdAt: string
}
export const listDebridAccounts = () => request<DebridAccount[]>('/api/debrid-accounts')

export const createDebridAccount = (input: { provider: 'realdebrid' | 'torbox'; apiKey: string }) =>
  request<DebridAccount>('/api/debrid-accounts', { method: 'POST', body: JSON.stringify(input) })

export const deleteDebridAccount = (id: string) =>
  request<void>(`/api/debrid-accounts/${id}`, { method: 'DELETE' })

export interface DebridItem {
  id: string
  libraryId?: string
  accountId: string
  sourceRef: string
  name: string
  status: 'resolving' | 'ready' | 'error' | 'removed'
  error?: string
  promoted: boolean
  addedAt: string
}
export const listDebridItems = () => request<DebridItem[]>('/api/debrid')

export interface AddDebridLinkInput {
  accountId: string
  sourceRef: string
  name?: string
  libraryId?: string
}
export const addDebridLink = (input: AddDebridLinkInput) =>
  request<DebridItem>('/api/debrid', { method: 'POST', body: JSON.stringify(input) })

export const removeDebridItem = (id: string) => request<void>(`/api/debrid/${id}`, { method: 'DELETE' })

export interface DebridFile {
  id: string
  name: string
  sizeBytes: number
  streamUrl: string
}
export const listDebridFiles = (itemId: string) => request<DebridFile[]>(`/api/debrid/${itemId}/files`)

export interface MaintenanceResult {
  cleared: number
  detail?: string
}
export const clearScanCache = () =>
  request<MaintenanceResult>('/api/admin/maintenance/clear-scan-cache', { method: 'POST' })
export const clearTranscodeCache = () =>
  request<MaintenanceResult>('/api/admin/maintenance/clear-transcode-cache', { method: 'POST' })

// logsStreamUrl builds the WebSocket URL for the live admin log viewer,
// carrying API_BASE's scheme (ws/wss mirrors http/https) since the log
// stream is a WebSocket, not a plain fetch.
export const logsStreamUrl = () => `${API_BASE.replace(/^http/, 'ws')}/api/admin/logs/stream`

// API_BASE is exported so components that need an absolute stream URL
// (e.g. the HLS player, which hands the URL to hls.js/a <video> element
// rather than fetching it themselves) can build one.
export { API_BASE }

# Vorn Media Server

Vorn is a self-hosted media server: library management, GPU-accelerated transcoding, a modern
web UI, and API compatibility with the official Plex, Emby, and Jellyfin apps — built to run
anywhere (Linux, macOS, Windows, Docker) and use whatever GPU hardware is available (Intel
QuickSync, Nvidia NVENC, AMD AMF/VAAPI, Apple VideoToolbox).

Vorn is under active, incremental development. This README and the feature list below will grow
as each phase lands — see [Roadmap](#roadmap) for what's done vs. planned.

## Why Vorn

Existing self-hosted media servers each pick a lane: Jellyfin is fully open source but slower to
add features some households want; Plex and Emby have client compatibility but aren't open source
end-to-end. Vorn aims to be a single, fully open-source (AGPL-3.0) server that:

- Speaks the native API of Plex, Emby, *and* Jellyfin apps, so existing mobile/TV/web clients
  just work without waiting for a Vorn-specific client.
- Scans large libraries fast, using a DragonflyDB staging layer in front of PostgreSQL so scan
  throughput isn't bottlenecked on database writes.
- Detects and uses GPU hardware transcoding automatically across all major vendors and OSes.
- Includes a built-in content acquisition system (torrents, NZB/Usenet, debrid providers) without
  requiring a separate Sonarr/Radarr-style stack.

## Feature Roadmap

Tracked in phases; each phase is delivered as a runnable increment with its own commits.

- [x] **Phase 0 — Foundations**: repo scaffolding, Docker Compose dev environment (Postgres +
      DragonflyDB + backend + frontend), Go backend skeleton, React/TS frontend skeleton with
      light/dark theme.
- [x] **Phase 1 — Core data model & auth**: schema, login/sessions, user management with
      per-library permissions, first-launch setup wizard.
- [x] **Phase 2 — Library scanner**: fast concurrent scanner with DragonflyDB staging, plus a
      dev-mode synthetic file generator for benchmarking scan speed at scale (verified at 50k
      files in ~3.6s locally).
- [x] **Phase 3 — Library management & dashboard UI**: folder mapping, admin header
      (profile/stats/search), continue-watching, per-library filters.
- [x] **Phase 4 — Metadata sync**: TMDb-backed art/trailers, manual metadata override.
- [x] **Phase 5 — Transcoder**: ffmpeg/ffprobe wrapper, GPU capability probing, on-the-fly HLS.
- [x] **Phase 6 — Player**: resume playback, autoplay-next-episode, currently-watching admin view.
- [x] **Phase 7 — Torrent acquisition**: anacrolix/torrent-backed client with sequential
      (streaming-order) or rarest-first download, a Torznab indexer plugin system for search,
      and an auto-add-to-library watcher on completion.
- [x] **Phase 8 — NZB & debrid acquisition**: hand-rolled NNTP/yEnc Usenet client with par2 repair,
      Real-Debrid/TorBox clients behind a shared `Provider.Resolve()` interface, and
      direct-stream-from-debrid playback with no local download step.
- [x] **Phase 9 — Client API compatibility**: Jellyfin (documented spec), Emby (near-free given
      Jellyfin's wire compatibility with its own fork origin), and Plex (reverse-engineered, no
      plex.tv integration — see the "Client API compatibility" section above for that limitation).
- [ ] **Phase 10 — Operability & distribution**: live logs, subtitles, custom domain/SSL, CDN
      support, self-updater, bare-metal installers.

## Architecture

```
vorn/
├── backend/     Go service — HTTP API, scanner, transcoder, acquisition, client-API shims
├── frontend/    React + TypeScript admin/viewer web UI (Vite)
├── deploy/      Docker Compose, Dockerfiles, GPU passthrough configs
└── docs/        Design notes per phase
```

- **Database**: PostgreSQL — libraries, users, metadata, per-user playback progress.
- **Cache / scan staging**: DragonflyDB (Redis-protocol compatible) — the scanner writes
  discovered files here first; a batching flusher moves them into Postgres, decoupling scan
  throughput from database write throughput.
- **Backend**: Go — chosen for concurrency (scanning, transcoding, torrent/NZB I/O all benefit),
  single static-binary distribution for bare-metal installers, and straightforward `ffmpeg`
  process management via `os/exec`.
- **Frontend**: React + TypeScript via Vite, with a CSS-variable-based theme system for
  light/dark mode.

## Development

Requires Docker + Docker Compose. GPU passthrough is optional for local dev.

```bash
cp deploy/.env.example deploy/.env
docker compose -f deploy/docker-compose.yml up --build
```

- Backend API: http://localhost:8080 (health check at `/healthz`)
- Frontend dev server: http://localhost:5173

### Optional environment variables

- `VORN_DEV_MODE=true` — enables the synthetic scan-benchmarking endpoint (`POST /api/dev/synthetic-scan`).
- `VORN_TMDB_API_KEY` — enables metadata sync (`POST /api/libraries/{id}/sync-metadata`). Without
  it, metadata sync is simply unavailable (503) rather than the server failing to start; get a
  free key at https://www.themoviedb.org/settings/api.
- `VORN_TRANSCODE_DIR` — where HLS output for active transcode sessions is written (default: a
  temp directory).
- `VORN_TRANSCODE_MAX_SESSIONS` — max concurrent transcode sessions (default: number of CPUs).
- `VORN_TORRENT_ENABLED=true` — enables torrent acquisition (`/api/torrents`, `/api/torrent-indexers`).
  Off by default since it opens a peer listening port and starts DHT.
- `VORN_TORRENT_DOWNLOAD_DIR` — where torrent data is saved (default: `./data/downloads`; the
  Docker Compose backend service always uses `/downloads`, backed by the `VORN_TORRENT_DOWNLOAD_PATH`
  host bind mount).
- `VORN_TORRENT_PEER_PORT` — TCP/uTP port for incoming peer connections (default: the
  anacrolix/torrent library default, 42069).
- `VORN_NZB_ENABLED=true` — enables NZB/Usenet acquisition (`/api/nzb`, `/api/usenet-servers`). Off
  by default; requires at least one enabled Usenet server to be configured via the admin UI before
  any download can start.
- `VORN_NZB_DOWNLOAD_DIR` — where NZB downloads are saved and par2-repaired (default:
  `./data/nzb-downloads`; the Docker Compose backend service always uses `/nzb-downloads`, backed
  by the `VORN_NZB_DOWNLOAD_PATH` host bind mount).
- `VORN_OPENSUBTITLES_API_KEY` / `VORN_OPENSUBTITLES_USERNAME` / `VORN_OPENSUBTITLES_PASSWORD` —
  enables subtitle integration (`GET /api/items/{id}/subtitles`, `GET /api/admin/subtitles/quota`).
  Requires a free OpenSubtitles.com API consumer key (https://www.opensubtitles.com/en/consumers)
  plus an account username/password (downloads need a logged-in account, not just the key). Without
  these, subtitle fetching is simply unavailable (503) rather than the server failing to start.
- `VORN_SUBTITLES_CACHE_DIR` — where downloaded subtitles are cached, keyed by the video file's
  content hash so a repeat request never touches the (metered) OpenSubtitles quota again (default:
  `./data/subtitles-cache`; the Docker Compose backend service always uses `/subtitles-cache`,
  backed by the `VORN_SUBTITLES_CACHE_PATH` host bind mount).

Debrid (Real-Debrid/TorBox) acquisition (`/api/debrid-accounts`, `/api/debrid`) has no env var and
is always available — it opens no listening port and holds no local state until the admin adds an
account (with its API key) via the admin UI. Resolving a magnet/hash produces direct provider CDN
stream URLs with no local download step, so playback of debrid content requires no download disk
space.

### Client API compatibility

Vorn also speaks a compatibility subset of the Jellyfin/Emby REST API directly (no env var, always
on, using the same admin/user accounts as Vorn's own UI): server discovery, `AuthenticateByName`,
library views, item browsing, poster/backdrop images, `PlaybackInfo`, direct-play video streaming,
and play-progress reporting. Point a Jellyfin *or* Emby client (official apps, Infuse, Findroid,
jellyfin-web) at Vorn's own base URL and it should authenticate and browse libraries as if talking
to a real server of that type. Emby is largely "free" here since Jellyfin is itself a fork of Emby
and kept wire compatibility (same paths, same `MediaBrowser ...` auth header, same JSON field
names) — every route is also registered under the `/emby` prefix real Emby clients/reverse proxies
conventionally use, and `/System/Info/Public` reports an Emby-flavored version string (`4.x`, not
Jellyfin's `10.x` scheme) when hit that way. Out of scope for now: Jellyfin/Emby's own HLS
transcode-session protocol (Vorn always offers a direct-play source; transcoding is handled by
Vorn's own player instead), search, collections/playlists/favorites, and user/library management
(use Vorn's `/api` admin surface for that).

Vorn also speaks a compatibility subset of the Plex Media Server API — the highest-risk of the
three since Plex has no official spec (field names/paths here come from Plex's own published Go
SDK, generated from Plex's real OpenAPI definitions): `/identity`, library sections, item
browsing, `/library/metadata/{ratingKey}` (probing the real file for its Media/Part info), direct
file streaming, and `/:/timeline` progress reporting. **Important limitation**: official Plex apps
discover servers by signing into plex.tv's cloud, which then tells the app which servers that
account can reach — Vorn is not a Plex-registered server and can't be, so official Plex mobile/TV
apps cannot be pointed at Vorn out of the box. What's implemented is the local, server-side
protocol for tools/clients that support manually configuring a Plex-protocol server + token,
including a `users/sign_in.json`-shaped auth shim (accepts HTTP Basic auth, a JSON body, or
classic form params) so such tooling can authenticate directly against Vorn instead of plex.tv.
Out of scope for now: Plex's transcode-decision/session protocol (same direct-play-only stance as
Jellyfin/Emby above), hubs, search, and collections.

### Admin: live logs and maintenance

Admin > Logs streams the server's own log output live over a WebSocket (`GET /api/admin/logs/stream`,
admin-only) — on connect it replays a scrollback buffer (last 2000 lines, in memory only, no log
file involved), then forwards new lines as they're logged. This works identically under Docker,
systemd, or a bare terminal. The same page exposes two maintenance actions: clearing stale
DragonflyDB scan-staging keys left behind by a crashed scan job (`POST
/api/admin/maintenance/clear-scan-cache`), and clearing finished transcode sessions whose tracking
and on-disk HLS output would otherwise leak forever once a session ends without an explicit stop
(`POST /api/admin/maintenance/clear-transcode-cache`).

### Admin: custom domain, automatic HTTPS, and Cloudflare

Admin > Network (`GET`/`PUT /api/admin/server-settings`) configures:

- **Custom domain + automatic HTTPS**: set a domain and enable SSL, and on the *next restart* Vorn
  uses [certmagic](https://github.com/caddyserver/certmagic) to automatically obtain and renew a
  Let's Encrypt certificate for it, serving HTTPS on port 443 with HTTP (port 80) redirecting to
  it — replacing the plain `VORN_HTTP_ADDR` listener entirely. Ports 80 and 443 must be reachable
  from the internet for that domain (ACME's HTTP-01 challenge needs it); this is not hot-reloaded,
  a domain/SSL change needs a restart to take effect.
- **Cloudflare-aware real IP**: if Vorn sits behind Cloudflare, enabling "trust Cloudflare" makes
  the `CF-Connecting-IP` header (visible in the access log line every request produces) reflect the
  actual visitor's IP instead of Cloudflare's own edge IP — but only when the connecting peer is
  itself a genuine Cloudflare edge IP (checked against ranges refreshed every 24h from Cloudflare's
  own public API), so the header can't simply be spoofed by any other client that sets it.

### Admin: self-update

Admin > Network also has a "Check for updates" / "Update to vX.Y.Z" control
(`GET /api/admin/update/check`, `POST /api/admin/update/apply`), backed by
[go-selfupdate](https://github.com/creativeprojects/go-selfupdate) checking `VORN_GITHUB_REPO`
(default this repo) for a newer GitHub Release than the running binary's version (set at build
time via `-ldflags "-X .../internal/version.Version=v1.2.3"`; a plain `go run`/`go build` reports
`0.0.0-dev`). Applying downloads and replaces the running executable in place but **does not
restart the process** — Vorn may have active playback sessions, so that's left to the admin (or
whatever process supervisor runs it). Checking always works; applying returns 409 under Docker,
where the container image is the unit of update instead of the binary inside it. Note this
requires the project to actually publish release binaries following go-selfupdate's naming
convention (`vornd_{os}_{arch}`) — a release pipeline for that isn't built yet.

### Running components natively (without Docker)

```bash
# Backend
cd backend && go run ./cmd/vornd

# Frontend
cd frontend && npm install && npm run dev
```

## License

Vorn Media Server is licensed under the [GNU Affero General Public License v3.0](LICENSE). The
AGPL was chosen deliberately: if you run a modified version of Vorn as a network service, you must
make your modified source available to its users. This keeps improvements to Vorn open, including
when it's offered as a hosted service.

## Contributing

Vorn is early and moving fast across many subsystems at once. Issues and PRs are welcome; see the
roadmap above for where things stand before proposing large changes.

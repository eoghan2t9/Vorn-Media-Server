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
- [ ] **Phase 7 — Torrent acquisition**: streaming-while-downloading torrent client, auto-add.
- [ ] **Phase 8 — NZB & debrid acquisition**: Usenet client, Real-Debrid/TorBox direct streaming.
- [ ] **Phase 9 — Client API compatibility**: Jellyfin, then Emby, then Plex.
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

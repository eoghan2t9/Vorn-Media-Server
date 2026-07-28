package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/acquisition"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/config"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/logging"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/notify"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/nzb"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/prowlarr"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/subtitles"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/sysstats"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/update"
	"github.com/google/uuid"
)

const sessionCookieName = "vorn_session"

// jellyfinWebDir is where backend.Dockerfile places Jellyfin's own official
// web client (see the Dockerfile's jellyfin-web build stage for why: the
// official Jellyfin apps expect a real server to host this, not just serve
// the JSON API). Only present in the Docker deploy image -- a plain
// `go run`/local dev build has no such directory, and NewRouter checks for
// its existence before registering anything there, so local dev is
// unaffected either way.
const jellyfinWebDir = "/usr/share/vorn/jellyfin-web"

// Deps bundles everything the HTTP layer needs. Metadata/Debrid/Notify are
// still handed in pre-built (credential-free or internally hot-reloading
// already); Config is the raw env-var config Server.reconfigure needs to
// (re)build TMDb/torrent/NZB/subtitles/acquisition itself -- see reconfigure
// for why those aren't pre-built here the way they used to be.
type Deps struct {
	Store        *store.Store
	Config       config.Config
	PostgresDSN  string
	BackupDir    string
	Scanner      *scanner.Service
	Metadata     *metadata.Service
	TranscodeMgr *transcode.Manager
	Debrid       *debrid.Service
	Notify       *notify.Service
	Update       *update.Service
	LogBuffer    *logging.Buffer
	SysStats     *sysstats.Sampler
	CORSOrigin   string
	DevMode      bool
}

type Server struct {
	store        *store.Store
	postgresDSN  string
	backupDir    string
	scanner      *scanner.Service
	metadataSvc  *metadata.Service
	tmdb         atomic.Pointer[metadata.TMDbClient]
	transcodeMgr *transcode.Manager
	torrentSvc   atomic.Pointer[torrent.Service]
	nzbSvc       atomic.Pointer[nzb.Service]
	debridSvc    *debrid.Service
	acquisition  atomic.Pointer[acquisition.Service]
	notify       *notify.Service
	subtitlesSvc atomic.Pointer[subtitles.Service]
	updateSvc    *update.Service
	logBuffer    *logging.Buffer
	sysStats     *sysstats.Sampler
	devMode      bool
	// serverID identifies this server to client-API-compatibility clients
	// (Jellyfin/Emby/Plex). It's regenerated on every restart, which is fine:
	// nothing depends on it surviving a restart except cosmetic "same
	// server?" UI checks in some clients.
	serverID string
	// trustCloudflare is read on every request by the access-log middleware,
	// so it's cached here (refreshed whenever the admin updates server
	// settings) rather than hitting Postgres per request.
	trustCloudflare atomic.Bool

	// baseCfg is the env-var config captured once at boot. reconfigure
	// merges it with the latest Admin > Integrations DB settings (which take
	// precedence when set) every time it runs -- TMDb/OpenSubtitles/Fanart/
	// OMDb/TVDb credentials and the torrent/NZB enable toggles all take
	// effect immediately this way, no restart needed.
	baseCfg config.Config
	// reconfigureMu serializes reconfigure() calls so starting/stopping the
	// same subsystem from two concurrent admin saves can't race.
	reconfigureMu sync.Mutex
	// monitorCancel stops the on-demand-acquisition MonitorScheduler
	// goroutine before a freshly (re)built acquisition service replaces the
	// old one -- guarded by reconfigureMu.
	monitorCancel context.CancelFunc
	// prowlarrCancel stops the running Prowlarr sync goroutine before a
	// fresh one is started against newly (re)built torrent/NZB instances --
	// guarded by reconfigureMu. nil whenever Prowlarr sync isn't running
	// (not configured, or neither torrent nor NZB is enabled).
	prowlarrCancel context.CancelFunc
}

func NewServer(deps Deps) *Server {
	s := &Server{
		store:        deps.Store,
		postgresDSN:  deps.PostgresDSN,
		backupDir:    deps.BackupDir,
		scanner:      deps.Scanner,
		metadataSvc:  deps.Metadata,
		transcodeMgr: deps.TranscodeMgr,
		debridSvc:    deps.Debrid,
		notify:       deps.Notify,
		updateSvc:    deps.Update,
		logBuffer:    deps.LogBuffer,
		sysStats:     deps.SysStats,
		devMode:      deps.DevMode,
		serverID:     uuid.NewString(),
		baseCfg:      deps.Config,
	}
	if settings, err := s.store.GetServerSettings(); err == nil {
		s.trustCloudflare.Store(settings.TrustCloudflare)
	}
	// Any item still 'searching'/'acquiring' at this point belongs to a
	// runAcquire goroutine that can't possibly still be running -- the
	// process that owned it just (re)started -- so it would otherwise be
	// stuck reporting "acquiring" forever with no retry path. See
	// store.ResetStuckAcquisitions.
	if n, err := s.store.ResetStuckAcquisitions(); err != nil {
		log.Printf("httpapi: resetting stuck acquisitions: %v", err)
	} else if n > 0 {
		log.Printf("httpapi: reset %d item(s) stuck in searching/acquiring from a previous run", n)
	}
	startCloudflareRangeRefresh()
	if err := s.reconfigure(); err != nil {
		log.Printf("httpapi: initial reconfigure: %v", err)
	}
	return s
}

// reconfigure (re)builds every credential-driven subsystem -- TMDb/Fanart/
// OMDb/TVDb-backed metadata providers, the standalone discover-search TMDb
// client, OpenSubtitles, torrent, NZB, and (derived from the last two)
// on-demand acquisition -- from baseCfg merged with the latest Admin >
// Integrations DB settings. Called once at NewServer time and again by
// handleUpdateIntegrationSettings after every save, so a credential change
// or an acquisition-source toggle takes effect on the very next request,
// no restart. Torrent/NZB are only actually started/stopped when their
// desired state changed since the last call, so an unrelated save (e.g.
// just a TMDb key edit) doesn't churn an already-running client.
func (s *Server) reconfigure() error {
	s.reconfigureMu.Lock()
	defer s.reconfigureMu.Unlock()

	intSettings, err := s.store.GetIntegrationSettings()
	if err != nil {
		return err
	}

	tmdbKey := s.baseCfg.TMDbAPIKey
	if intSettings.TMDbAPIKey != "" {
		tmdbKey = intSettings.TMDbAPIKey
	}
	tvdbKey, tvdbPin := s.baseCfg.TVDbAPIKey, s.baseCfg.TVDbPin
	if intSettings.TVDbAPIKey != "" {
		tvdbKey = intSettings.TVDbAPIKey
	}
	if intSettings.TVDbPin != "" {
		tvdbPin = intSettings.TVDbPin
	}
	fanartKey := s.baseCfg.FanartAPIKey
	if intSettings.FanartAPIKey != "" {
		fanartKey = intSettings.FanartAPIKey
	}
	omdbKey := s.baseCfg.OMDbAPIKey
	if intSettings.OMDbAPIKey != "" {
		omdbKey = intSettings.OMDbAPIKey
	}
	osKey, osUser, osPass := s.baseCfg.OpenSubtitlesAPIKey, s.baseCfg.OpenSubtitlesUser, s.baseCfg.OpenSubtitlesPass
	if intSettings.OpenSubtitlesAPIKey != "" {
		osKey = intSettings.OpenSubtitlesAPIKey
	}
	if intSettings.OpenSubtitlesUsername != "" {
		osUser = intSettings.OpenSubtitlesUsername
	}
	if intSettings.OpenSubtitlesPassword != "" {
		osPass = intSettings.OpenSubtitlesPassword
	}
	torrentEnabled := s.baseCfg.TorrentEnabled
	if intSettings.TorrentEnabled != nil {
		torrentEnabled = *intSettings.TorrentEnabled
	}
	nzbEnabled := s.baseCfg.NZBEnabled
	if intSettings.NZBEnabled != nil {
		nzbEnabled = *intSettings.NZBEnabled
	}

	s.metadataSvc.Reconfigure(metadata.ProviderConfig{
		TMDbAPIKey: tmdbKey, TVDbAPIKey: tvdbKey, TVDbPin: tvdbPin, FanartAPIKey: fanartKey, OMDbAPIKey: omdbKey,
	})

	if tmdbKey != "" {
		s.tmdb.Store(metadata.NewTMDbClient(tmdbKey))
	} else {
		s.tmdb.Store(nil)
	}

	if osKey != "" && osUser != "" {
		subSvc, err := subtitles.NewService(osKey, osUser, osPass, s.baseCfg.SubtitlesCacheDir)
		if err != nil {
			log.Printf("httpapi: reconfiguring subtitles service: %v", err)
			s.subtitlesSvc.Store(nil)
		} else {
			s.subtitlesSvc.Store(subSvc)
		}
	} else {
		s.subtitlesSvc.Store(nil)
	}

	// torrentChanged/nzbChanged track whether the *instance* actually
	// changed identity this call (started, stopped, or cycled) -- used
	// below to decide whether Prowlarr sync needs to be restarted against
	// a fresh instance, not just whether it's still enabled.
	torrentChanged := false
	if torrentEnabled && s.torrentSvc.Load() == nil {
		ts, err := torrent.NewService(s.store, s.baseCfg.TorrentDownloadDir, s.baseCfg.TorrentPeerPort, s.debridSvc.TorBoxLimiter())
		if err != nil {
			log.Printf("httpapi: starting torrent service: %v", err)
		} else {
			s.torrentSvc.Store(ts)
			torrentChanged = true
		}
	} else if !torrentEnabled {
		if old := s.torrentSvc.Swap(nil); old != nil {
			old.Close()
			torrentChanged = true
		}
	}

	nzbChanged := false
	if nzbEnabled && s.nzbSvc.Load() == nil {
		s.nzbSvc.Store(nzb.NewService(s.store, s.debridSvc.TorBoxLimiter()))
		nzbChanged = true
	} else if !nzbEnabled {
		if old := s.nzbSvc.Swap(nil); old != nil {
			nzbChanged = true
		}
	}

	tmdbClient, torrentSvc, nzbSvc := s.tmdb.Load(), s.torrentSvc.Load(), s.nzbSvc.Load()
	// Acquisition needs TMDb (to materialize placeholders) plus at least
	// one acquisition source -- torrent+debrid or NZB/Usenet -- not
	// necessarily torrent specifically, so an NZB-only setup (no torrent
	// indexers at all) still gets on-demand acquisition.
	wantAcquisition := tmdbClient != nil && (torrentSvc != nil || nzbSvc != nil)
	current := s.acquisition.Load()
	// Rebuild whenever existence should change, or the underlying tmdb/
	// torrent/nzb instances changed identity (e.g. torrent or NZB was
	// cycled off then on) -- comparing by whether current is nil is enough
	// for the existence flip; an unrelated reconfigure (say, just an OMDb
	// key edit) leaves all three pointers untouched, so this is a no-op then.
	if wantAcquisition != (current != nil) || torrentChanged || nzbChanged {
		if s.monitorCancel != nil {
			s.monitorCancel()
			s.monitorCancel = nil
		}
		if wantAcquisition {
			acq := acquisition.NewService(s.store, tmdbClient, torrentSvc, nzbSvc, s.debridSvc, s.notify)
			s.acquisition.Store(acq)
			ctx, cancel := context.WithCancel(context.Background())
			s.monitorCancel = cancel
			go acq.NewMonitorScheduler(s.store).Run(ctx)
		} else {
			s.acquisition.Store(nil)
		}
	}

	// Prowlarr sync (env/compose-only -- no admin UI field, so its own
	// config never changes at runtime) mirrors indexers into whichever of
	// torrentSvc/nzbSvc are enabled. Restart it whenever either instance
	// actually changed identity (a fresh instance needs a fresh sync
	// pointed at it) or it isn't running yet but now should be; tear it
	// down if neither acquisition source is enabled anymore.
	wantProwlarr := s.baseCfg.ProwlarrBaseURL != "" &&
		(s.baseCfg.ProwlarrAPIKey != "" || s.baseCfg.ProwlarrConfigPath != "") &&
		(torrentSvc != nil || nzbSvc != nil)
	if wantProwlarr && (s.prowlarrCancel == nil || torrentChanged || nzbChanged) {
		if s.prowlarrCancel != nil {
			s.prowlarrCancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.prowlarrCancel = cancel
		go prowlarr.NewSyncService(torrentSvc, nzbSvc, s.baseCfg.ProwlarrBaseURL, s.baseCfg.ProwlarrAPIKey, s.baseCfg.ProwlarrConfigPath).Run(ctx)
	} else if !wantProwlarr && s.prowlarrCancel != nil {
		s.prowlarrCancel()
		s.prowlarrCancel = nil
	}

	return nil
}

// accessLog logs one line per request (method, path, status, duration, and
// the request's real client IP -- Cloudflare-aware if the admin has enabled
// that, see realClientIP) into the same buffer the admin live-logs viewer
// tails, giving Vorn a basic access log with no separate log file.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		ip := realClientIP(r, s.trustCloudflare.Load())
		log.Printf("%s %s %s %d %s", ip, r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Hijack forwards to the underlying ResponseWriter's http.Hijacker.
// Without this, statusRecorder only satisfies the plain http.ResponseWriter
// interface (Go's interface embedding doesn't promote methods outside that
// interface's own method set, regardless of what the wrapped concrete value
// supports) -- so gorilla/websocket's Upgrade, which needs to hijack the
// raw TCP connection, would fail its own internal type assertion and every
// WebSocket route behind this middleware (e.g. the admin log stream) would
// 500 on every connection attempt.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
	}
	return hj.Hijack()
}

// NewRouter returns the root HTTP handler for the Vorn backend. Prowlarr
// sync is now wired up inside reconfigure (it restarts itself whenever
// torrent/NZB change), so callers no longer need the underlying *Server.
func NewRouter(deps Deps) http.Handler {
	s := NewServer(deps)
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", handleHealthz)

	mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/init", s.handleSetupInit)

	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.withAuth(s.handleMe))

	mux.HandleFunc("GET /api/users", s.withAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/users", s.withAdmin(s.handleCreateUser))
	mux.HandleFunc("PATCH /api/users/{id}", s.withAdmin(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/users/{id}", s.withAdmin(s.handleDeleteUser))
	mux.HandleFunc("PUT /api/users/{id}/permissions", s.withAdmin(s.handleSetUserPermissions))

	mux.HandleFunc("GET /api/libraries", s.withAuth(s.handleListLibraries))
	mux.HandleFunc("POST /api/libraries", s.withAdmin(s.handleCreateLibrary))
	mux.HandleFunc("GET /api/libraries/{id}", s.withAuth(s.handleGetLibrary))
	mux.HandleFunc("PATCH /api/libraries/{id}", s.withAdmin(s.handleUpdateLibrary))
	mux.HandleFunc("DELETE /api/libraries/{id}", s.withAdmin(s.handleDeleteLibrary))
	mux.HandleFunc("GET /api/libraries/{id}/items", s.withAuth(s.handleListLibraryItems))
	mux.HandleFunc("GET /api/admin/browse", s.withAdmin(s.handleBrowseFilesystem))

	mux.HandleFunc("POST /api/libraries/{id}/scan", s.withAdmin(s.handleStartLibraryScan))
	mux.HandleFunc("GET /api/scan-jobs", s.withAdmin(s.handleListScanJobs))
	mux.HandleFunc("GET /api/scan-jobs/{id}", s.withAdmin(s.handleGetScanJob))
	mux.HandleFunc("POST /api/dev/synthetic-scan", s.withAdmin(s.handleSyntheticScan))

	mux.HandleFunc("GET /api/items/{id}", s.withAuth(s.handleGetItem))
	mux.HandleFunc("PUT /api/items/{id}/monitor", s.withAuth(s.handleSetItemMonitored))
	mux.HandleFunc("PUT /api/items/{id}/progress", s.withAuth(s.handleUpdateProgress))
	// Same handler, also reachable via POST: navigator.sendBeacon (used for
	// the last-gasp progress save on tab close/navigation, since it's the
	// only API that reliably survives page unload) can only send POST, and
	// can't set a method the way fetch can.
	mux.HandleFunc("POST /api/items/{id}/progress", s.withAuth(s.handleUpdateProgress))
	mux.HandleFunc("GET /api/items/{id}/progress", s.withAuth(s.handleGetProgress))
	mux.HandleFunc("GET /api/continue-watching", s.withAuth(s.handleContinueWatching))

	mux.HandleFunc("GET /api/admin/stats", s.withAdmin(s.handleServerStats))
	mux.HandleFunc("GET /api/admin/stats/system", s.withAdmin(s.handleSystemStats))
	mux.HandleFunc("GET /api/admin/currently-watching", s.withAdmin(s.handleCurrentlyWatching))
	mux.HandleFunc("GET /api/search", s.withAuth(s.handleSearch))

	mux.HandleFunc("GET /api/discover/search", s.withAuth(s.handleDiscoverSearch))
	mux.HandleFunc("GET /api/browse", s.withAuth(s.handleBrowseCatalog))
	mux.HandleFunc("POST /api/browse/open", s.withAuth(s.handleOpenCatalogEntry))
	mux.HandleFunc("GET /api/libraries/{id}/quality-profile", s.withAdmin(s.handleGetQualityProfile))
	mux.HandleFunc("PUT /api/libraries/{id}/quality-profile", s.withAdmin(s.handleUpdateQualityProfile))
	mux.HandleFunc("POST /api/requests", s.withAuth(s.handleCreateContentRequest))
	mux.HandleFunc("GET /api/requests", s.withAuth(s.handleListMyContentRequests))
	mux.HandleFunc("DELETE /api/requests/{id}", s.withAuth(s.handleDeleteContentRequest))
	mux.HandleFunc("GET /api/admin/requests", s.withAdmin(s.handleListAdminContentRequests))
	mux.HandleFunc("GET /api/admin/requests/settings", s.withAdmin(s.handleGetRequestSettings))
	mux.HandleFunc("PUT /api/admin/requests/settings", s.withAdmin(s.handleUpdateRequestSettings))
	mux.HandleFunc("PUT /api/admin/requests/{id}", s.withAdmin(s.handleDecideContentRequest))
	mux.HandleFunc("DELETE /api/admin/requests/{id}", s.withAdmin(s.handleAdminDeleteContentRequest))

	mux.HandleFunc("POST /api/libraries/{id}/sync-metadata", s.withAdmin(s.handleStartMetadataSync))
	mux.HandleFunc("GET /api/metadata-jobs/{id}", s.withAdmin(s.handleGetMetadataJob))
	mux.HandleFunc("PATCH /api/items/{id}/metadata", s.withAdmin(s.handleUpdateItemMetadata))

	mux.HandleFunc("GET /api/transcode/capabilities", s.withAuth(s.handleTranscodeCapabilities))
	mux.HandleFunc("POST /api/items/{id}/play", s.withAuth(s.handlePlayItem))
	mux.HandleFunc("GET /api/stream/direct/{id}", s.withAuth(s.handleDirectStream))
	mux.HandleFunc("GET /api/stream/session/{sessionId}/{file}", s.withAuth(s.handleSessionFile))
	mux.HandleFunc("GET /api/artwork/{key}", s.withAuth(s.handleArtwork))
	mux.HandleFunc("DELETE /api/stream/session/{sessionId}", s.withAuth(s.handleStopSession))

	mux.HandleFunc("GET /api/torrents", s.withAdmin(s.handleListTorrents))
	mux.HandleFunc("POST /api/torrents", s.withAdmin(s.handleAddMagnet))
	mux.HandleFunc("POST /api/torrents/file", s.withAdmin(s.handleAddTorrentFile))
	mux.HandleFunc("DELETE /api/torrents/{id}", s.withAdmin(s.handleRemoveTorrent))
	mux.HandleFunc("GET /api/torrents/search", s.withAdmin(s.handleTorrentSearch))
	mux.HandleFunc("GET /api/torrent-indexers", s.withAdmin(s.handleListTorrentIndexers))
	mux.HandleFunc("POST /api/torrent-indexers", s.withAdmin(s.handleCreateTorrentIndexer))
	mux.HandleFunc("POST /api/torrent-indexers/test", s.withAdmin(s.handleTestTorrentIndexer))
	mux.HandleFunc("PATCH /api/torrent-indexers/{id}", s.withAdmin(s.handleUpdateTorrentIndexer))
	mux.HandleFunc("DELETE /api/torrent-indexers/{id}", s.withAdmin(s.handleDeleteTorrentIndexer))

	mux.HandleFunc("GET /api/nzb", s.withAdmin(s.handleListNZBDownloads))
	mux.HandleFunc("POST /api/nzb", s.withAdmin(s.handleAddNZB))
	mux.HandleFunc("DELETE /api/nzb/{id}", s.withAdmin(s.handleRemoveNZB))
	mux.HandleFunc("GET /api/nzb/search", s.withAdmin(s.handleNZBSearch))
	mux.HandleFunc("POST /api/nzb/from-url", s.withAdmin(s.handleAddNZBFromURL))
	mux.HandleFunc("GET /api/nzb-indexers", s.withAdmin(s.handleListNZBIndexers))
	mux.HandleFunc("POST /api/nzb-indexers", s.withAdmin(s.handleCreateNZBIndexer))
	mux.HandleFunc("POST /api/nzb-indexers/test", s.withAdmin(s.handleTestNZBIndexer))
	mux.HandleFunc("PATCH /api/nzb-indexers/{id}", s.withAdmin(s.handleUpdateNZBIndexer))
	mux.HandleFunc("DELETE /api/nzb-indexers/{id}", s.withAdmin(s.handleDeleteNZBIndexer))
	mux.HandleFunc("GET /api/usenet-servers", s.withAdmin(s.handleListUsenetServers))
	mux.HandleFunc("POST /api/usenet-servers", s.withAdmin(s.handleCreateUsenetServer))
	mux.HandleFunc("POST /api/usenet-servers/test", s.withAdmin(s.handleTestUsenetServer))
	mux.HandleFunc("PATCH /api/usenet-servers/{id}", s.withAdmin(s.handleUpdateUsenetServer))
	mux.HandleFunc("DELETE /api/usenet-servers/{id}", s.withAdmin(s.handleDeleteUsenetServer))

	mux.HandleFunc("GET /api/debrid-accounts", s.withAdmin(s.handleListDebridAccounts))
	mux.HandleFunc("POST /api/debrid-accounts", s.withAdmin(s.handleCreateDebridAccount))
	mux.HandleFunc("POST /api/debrid-accounts/test", s.withAdmin(s.handleTestDebridAccount))
	mux.HandleFunc("DELETE /api/debrid-accounts/{id}", s.withAdmin(s.handleDeleteDebridAccount))
	mux.HandleFunc("GET /api/debrid", s.withAdmin(s.handleListDebridItems))
	mux.HandleFunc("POST /api/debrid", s.withAdmin(s.handleAddDebridLink))
	mux.HandleFunc("DELETE /api/debrid/{id}", s.withAdmin(s.handleRemoveDebridItem))
	mux.HandleFunc("GET /api/debrid/{id}/files", s.withAdmin(s.handleListDebridFiles))

	// Jellyfin/Emby-compatible client API (see internal/jellyfin's doc
	// comment for scope). These paths are dictated by the MediaBrowser
	// protocol Jellyfin and Emby both speak (Jellyfin is a fork of Emby and
	// kept wire compatibility: same paths, same "MediaBrowser ..." auth
	// header, same JSON field names), not Vorn's own conventions, so they
	// intentionally don't live under /api. Real Emby clients and reverse
	// proxies conventionally address the same server under an "/emby"
	// prefix, so every route is registered both ways; handleJfPublicSystemInfo
	// inspects which prefix was used to report Emby- vs Jellyfin-flavored
	// server identity (some clients gate features on the version scheme).
	jfRoutes := []struct {
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"GET", "/System/Info/Public", s.handleJfPublicSystemInfo},
		{"GET", "/QuickConnect/Enabled", s.handleJfQuickConnectEnabled},
		{"GET", "/Users/Public", s.handleJfPublicUsers},
		{"GET", "/Branding/Configuration", s.handleJfBrandingConfiguration},
		{"POST", "/Users/AuthenticateByName", s.handleJfAuthenticateByName},
		{"GET", "/Users/{id}", s.withJellyfinAuth(s.handleJfUser)},
		{"GET", "/Users/{userId}/Views", s.withJellyfinAuth(s.handleJfUserViews)},
		{"GET", "/Users/{userId}/Items", s.withJellyfinAuth(s.handleJfItems)},
		{"GET", "/Items", s.withJellyfinAuth(s.handleJfItems)},
		{"GET", "/Users/{userId}/Items/{id}", s.withJellyfinAuth(s.handleJfItem)},
		{"GET", "/Items/{id}", s.withJellyfinAuth(s.handleJfItem)},
		{"GET", "/Items/{id}/Images/{type}", s.handleJfItemImage},
		{"GET", "/Items/{id}/PlaybackInfo", s.withJellyfinAuth(s.handleJfPlaybackInfo)},
		{"POST", "/Items/{id}/PlaybackInfo", s.withJellyfinAuth(s.handleJfPlaybackInfo)},
		{"GET", "/Videos/{id}/{filename}", s.withJellyfinAuth(s.handleJfVideoStream)},
		{"POST", "/Sessions/Playing", s.withJellyfinAuth(s.jfUpdateProgress)},
		{"POST", "/Sessions/Playing/Progress", s.withJellyfinAuth(s.jfUpdateProgress)},
		{"POST", "/Sessions/Playing/Stopped", s.withJellyfinAuth(s.jfUpdateProgress)},
		{"POST", "/Sessions/Capabilities", s.withJellyfinAuth(s.handleJfCapabilities)},
		{"POST", "/Sessions/Capabilities/Full", s.withJellyfinAuth(s.handleJfCapabilities)},
		{"GET", "/DisplayPreferences/{id}", s.withJellyfinAuth(s.handleJfGetDisplayPreferences)},
		{"POST", "/DisplayPreferences/{id}", s.withJellyfinAuth(s.handleJfUpdateDisplayPreferences)},
		{"GET", "/socket", s.withJellyfinAuth(s.handleJfSocket)},
	}
	// jfTemplates covers BOTH the bare and /emby-prefixed forms of every
	// route -- confirmed live that even the genuine, official jellyfin-web
	// client (not just the Emby app) sends a different case than what's
	// registered here (it POSTs "/Users/authenticatebyname", all lowercase,
	// against a route registered as "/Users/AuthenticateByName"), so this
	// isn't an Emby-only quirk. See jfPathCanonicalizer.
	jfTemplates := make([]string, 0, len(jfRoutes)*2)
	for _, rt := range jfRoutes {
		mux.HandleFunc(rt.method+" "+rt.path, rt.handler)
		mux.HandleFunc(rt.method+" /emby"+rt.path, rt.handler)
		jfTemplates = append(jfTemplates, rt.path, "/emby"+rt.path)
	}

	// Plex-compatible client API (see internal/plex's doc comment for scope
	// and its one real limitation: no plex.tv integration, so official Plex
	// apps can't discover Vorn -- this targets tooling that supports
	// manually configuring a Plex-protocol server + token).
	mux.HandleFunc("GET /identity", s.handlePlexIdentity)
	mux.HandleFunc("POST /users/sign_in.json", s.handlePlexSignIn)
	mux.HandleFunc("POST /users/sign_in", s.handlePlexSignIn)
	mux.HandleFunc("GET /library/sections", s.withPlexAuth(s.handlePlexSections))
	mux.HandleFunc("GET /library/sections/{sectionId}/all", s.withPlexAuth(s.handlePlexSectionItems))
	mux.HandleFunc("GET /library/metadata/{ratingKey}", s.withPlexAuth(s.handlePlexMetadataItem))
	mux.HandleFunc("GET /library/metadata/{ratingKey}/children", s.withPlexAuth(s.handlePlexMetadataChildren))
	mux.HandleFunc("GET /library/parts/{id}/{filename}", s.withPlexAuth(s.handlePlexPartFile))
	mux.HandleFunc("GET /:/timeline", s.withPlexAuth(s.handlePlexTimeline))
	mux.HandleFunc("POST /:/timeline", s.withPlexAuth(s.handlePlexTimeline))

	mux.HandleFunc("GET /api/admin/logs/stream", s.withAdmin(s.handleAdminLogsStream))
	mux.HandleFunc("POST /api/admin/maintenance/clear-scan-cache", s.withAdmin(s.handleClearScanCache))
	mux.HandleFunc("POST /api/admin/maintenance/clear-transcode-cache", s.withAdmin(s.handleClearTranscodeCache))

	mux.HandleFunc("GET /api/items/{id}/subtitles", s.withAuth(s.handleGetSubtitles))
	mux.HandleFunc("GET /api/admin/subtitles/quota", s.withAdmin(s.handleSubtitlesQuota))

	mux.HandleFunc("GET /api/admin/server-settings", s.withAdmin(s.handleGetServerSettings))
	mux.HandleFunc("PUT /api/admin/server-settings", s.withAdmin(s.handleUpdateServerSettings))

	mux.HandleFunc("GET /api/admin/integrations", s.withAdmin(s.handleGetIntegrationSettings))
	mux.HandleFunc("PUT /api/admin/integrations", s.withAdmin(s.handleUpdateIntegrationSettings))

	mux.HandleFunc("GET /api/admin/update/check", s.withAdmin(s.handleCheckForUpdate))
	mux.HandleFunc("POST /api/admin/update/apply", s.withAdmin(s.handleApplyUpdate))
	mux.HandleFunc("POST /api/admin/restart", s.withAdmin(s.handleRestartServer))

	mux.HandleFunc("GET /api/admin/backup", s.withAdmin(s.handleDownloadBackup))
	mux.HandleFunc("POST /api/admin/backup/restore", s.withAdmin(s.handleRestoreBackup))

	mux.HandleFunc("GET /api/admin/backups/settings", s.withAdmin(s.handleGetBackupSettings))
	mux.HandleFunc("PUT /api/admin/backups/settings", s.withAdmin(s.handleUpdateBackupSettings))

	mux.HandleFunc("GET /api/admin/notifications", s.withAdmin(s.handleGetNotificationSettings))
	mux.HandleFunc("PUT /api/admin/notifications", s.withAdmin(s.handleUpdateNotificationSettings))
	mux.HandleFunc("POST /api/admin/notifications/test", s.withAdmin(s.handleTestNotification))
	mux.HandleFunc("GET /api/admin/backups", s.withAdmin(s.handleListAutoBackups))
	mux.HandleFunc("GET /api/admin/backups/{filename}", s.withAdmin(s.handleDownloadAutoBackup))
	mux.HandleFunc("DELETE /api/admin/backups/{filename}", s.withAdmin(s.handleDeleteAutoBackup))
	mux.HandleFunc("POST /api/admin/backups/{filename}/restore", s.withAdmin(s.handleRestoreAutoBackup))

	// Registered last, as a catch-all -- Go's ServeMux always prefers the
	// most specific matching pattern regardless of registration order, so
	// this only ever serves paths nothing above claimed (in practice: "/"
	// and the web client's own static asset requests). See jellyfinWebDir's
	// doc comment for why this exists and why it's fine to skip entirely
	// when the directory isn't there (local dev).
	if info, err := os.Stat(jellyfinWebDir); err == nil && info.IsDir() {
		mux.Handle("/", jellyfinWebHandler(jellyfinWebDir))
		// Without this, the bare "/" catch-all above would also swallow any
		// unmatched "/emby/*" request (Go's mux has no other "/emby/..."
		// subtree registered, only exact jfRoutes literals) and serve
		// Jellyfin's web client mislabeled under Emby's own path -- confirmed
		// live that the official Emby app partially loads it, notices it
		// isn't genuine Emby content, and reports the server as
		// "unsupported, needs updated" instead of the plain 404 it got
		// before. A longer registered prefix always beats the shorter "/"
		// catch-all in Go's mux, so this takes priority for the whole
		// /emby/ subtree and gives a clean, honest 404 instead.
		mux.HandleFunc("/emby/", func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, "not found")
		})
	}

	return s.accessLog(withCORS(jfPathCanonicalizer(mux, jfTemplates), deps.CORSOrigin))
}

// jellyfinWebHandler serves Jellyfin's bundled web client as static files,
// falling back to index.html for any path that isn't a real file on disk --
// the standard client-side-routed-SPA hosting pattern, since jellyfin-web
// handles its own in-app navigation via the History API rather than the
// server knowing about every possible route.
func jellyfinWebHandler(webDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(webDir))
	indexPath := filepath.Join(webDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		full := filepath.Join(webDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	})
}

// canonicalizeJfPath finds a template in templates whose static path
// segments case-insensitively match reqPath's, and returns the canonical
// form: the template's own casing for static segments (recovering whatever
// the client mangled) and the client's original value for {param} segments
// (so a real path parameter -- an item ID, a filename -- round-trips
// exactly as given; only structure/casing is normalized). ok is false if
// reqPath doesn't structurally match any template at all (different
// segment count, or a static segment differs even case-insensitively), in
// which case reqPath is returned unchanged and normal routing 404s it like
// any other unknown path.
func canonicalizeJfPath(reqPath string, templates []string) (canonical string, ok bool) {
	reqSegs := strings.Split(strings.TrimPrefix(reqPath, "/"), "/")
	for _, tmpl := range templates {
		tmplSegs := strings.Split(strings.TrimPrefix(tmpl, "/"), "/")
		if len(tmplSegs) != len(reqSegs) {
			continue
		}
		match := true
		out := make([]string, len(tmplSegs))
		for i, ts := range tmplSegs {
			if strings.HasPrefix(ts, "{") && strings.HasSuffix(ts, "}") {
				out[i] = reqSegs[i]
				continue
			}
			if !strings.EqualFold(ts, reqSegs[i]) {
				match = false
				break
			}
			out[i] = ts
		}
		if match {
			return "/" + strings.Join(out, "/"), true
		}
	}
	return reqPath, false
}

// jfPathCanonicalizer rewrites r.URL.Path to the correctly-cased route
// before the mux ever sees it -- confirmed live that BOTH the official
// Emby app (lowercases everything: "/emby/system/info/public") AND the
// genuine, official jellyfin-web client (POSTs "/Users/authenticatebyname",
// all lowercase, against a route registered as
// "/Users/AuthenticateByName") send different casing than what's
// registered here, and Go's ServeMux matches static path segments
// case-sensitively, so every one of those requests 404'd -- for
// jellyfin-web's case, worse: it silently fell through to the "/"
// catch-all's SPA fallback instead, serving index.html with a 200 instead
// of ever reaching the real login handler. Applied to every request, not
// just "/emby/", since that bare-path case proved this isn't Emby-specific;
// canonicalizeJfPath only ever rewrites on a genuine structural match
// against a real jfRoutes template, so this is a no-op for every other path
// (the whole rest of Vorn's own API, etc).
func jfPathCanonicalizer(next http.Handler, templates []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if canonical, ok := canonicalizeJfPath(r.URL.Path, templates); ok {
			r.URL.Path = canonical
			r.URL.RawPath = "" // a stale RawPath would otherwise override Path during routing
		}
		next.ServeHTTP(w, r)
	})
}

// withCORS allows the frontend dev server (or, in production, whatever
// origin the admin configures) to make credentialed (cookie-based) requests.
// Access-Control-Allow-Origin can't be "*" when credentials are allowed, so
// the configured origin is echoed back explicitly.
func withCORS(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			log.Printf("httpapi: encoding response: %v", err)
		}
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

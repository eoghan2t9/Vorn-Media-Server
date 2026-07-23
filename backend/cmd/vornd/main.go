// Command vornd is the Vorn Media Server backend daemon.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/caddyserver/certmagic"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/config"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/httpapi"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/logging"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/migrate"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/nzb"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/subtitles"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/update"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/version"
)

// logBufferLines caps how much log history the admin live-logs viewer can
// show on connect; older lines are simply dropped, not persisted anywhere.
const logBufferLines = 2000

func main() {
	logBuffer := logging.NewBuffer(os.Stdout, logBufferLines)
	log.SetOutput(logBuffer)

	cfg := config.Load()
	log.Printf("vornd starting: %s", cfg)

	if err := migrate.Up(cfg.PostgresDSN); err != nil {
		log.Fatalf("migrations failed: %v", err)
	}
	log.Print("migrations up to date")

	st, err := store.Open(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer st.Close()

	queue := scanner.NewQueue(cfg.DragonflyAddr)
	if err := queue.Ping(context.Background()); err != nil {
		log.Fatalf("connecting to dragonfly: %v", err)
	}
	defer queue.Close()

	scanSvc := scanner.NewService(st, queue)

	var metadataSvc *metadata.Service
	if cfg.TMDbAPIKey != "" {
		metadataSvc = metadata.NewService(st, metadata.NewTMDbProvider(cfg.TMDbAPIKey))
	} else {
		log.Print("VORN_TMDB_API_KEY not set: metadata sync is disabled")
	}

	var transcodeMgr *transcode.Manager
	backends := transcode.DetectBackends(context.Background())
	if len(backends) == 0 {
		log.Print("no working ffmpeg encoder found (checked hardware + software): transcoding is disabled")
	} else {
		names := make([]string, len(backends))
		for i, b := range backends {
			names[i] = b.Name
		}
		log.Printf("transcoder backends available: %v", names)
		if err := os.MkdirAll(cfg.TranscodeOutputDir, 0o755); err != nil {
			log.Fatalf("creating transcode output dir: %v", err)
		}
		transcodeMgr = transcode.NewManager(cfg.TranscodeOutputDir, backends, cfg.TranscodeMaxSessions)
	}

	var torrentSvc *torrent.Service
	if cfg.TorrentEnabled {
		torrentSvc, err = torrent.NewService(st, cfg.TorrentDownloadDir, cfg.TorrentPeerPort)
		if err != nil {
			log.Fatalf("starting torrent service: %v", err)
		}
		defer torrentSvc.Close()
	} else {
		log.Print("VORN_TORRENT_ENABLED not set: torrent acquisition is disabled")
	}

	var nzbSvc *nzb.Service
	if cfg.NZBEnabled {
		nzbSvc, err = nzb.NewService(st, cfg.NZBDownloadDir)
		if err != nil {
			log.Fatalf("starting nzb service: %v", err)
		}
	} else {
		log.Print("VORN_NZB_ENABLED not set: NZB acquisition is disabled")
	}

	// Debrid (Real-Debrid/TorBox) has no listening port or background
	// resources to gate behind an enable flag; it's a no-op until the admin
	// configures at least one account.
	debridSvc := debrid.NewService(st)

	var subtitlesSvc *subtitles.Service
	if cfg.OpenSubtitlesAPIKey != "" && cfg.OpenSubtitlesUser != "" {
		subtitlesSvc, err = subtitles.NewService(cfg.OpenSubtitlesAPIKey, cfg.OpenSubtitlesUser, cfg.OpenSubtitlesPass, cfg.SubtitlesCacheDir)
		if err != nil {
			log.Fatalf("starting subtitles service: %v", err)
		}
	} else {
		log.Print("VORN_OPENSUBTITLES_API_KEY/VORN_OPENSUBTITLES_USERNAME not set: subtitle integration is disabled")
	}

	if update.IsDockerized() {
		log.Print("running under Docker: self-update is a no-op (rebuild/pull the image instead)")
	}
	updateSvc := update.NewService(cfg.GitHubRepo, version.Version)

	router := httpapi.NewRouter(httpapi.Deps{
		Store:        st,
		Scanner:      scanSvc,
		Metadata:     metadataSvc,
		TranscodeMgr: transcodeMgr,
		Torrent:      torrentSvc,
		NZB:          nzbSvc,
		Debrid:       debridSvc,
		Subtitles:    subtitlesSvc,
		Update:       updateSvc,
		LogBuffer:    logBuffer,
		CORSOrigin:   cfg.CORSOrigin,
		DevMode:      cfg.DevMode,
	})

	settings, err := st.GetServerSettings()
	if err != nil {
		log.Fatalf("loading server settings: %v", err)
	}

	if settings.SSLEnabled && settings.CustomDomain != "" {
		// certmagic.HTTPS is a blocking call: it binds :80 (ACME HTTP-01
		// challenge + redirect to HTTPS) and :443 (TLS) itself, replacing
		// the plain cfg.HTTPAddr listener entirely -- both ports must be
		// reachable from the internet for the domain for issuance/renewal
		// to succeed. A custom domain/SSL change only takes effect on the
		// next restart; this isn't hot-reloaded.
		if settings.ACMEEmail != "" {
			certmagic.DefaultACME.Email = settings.ACMEEmail
		}
		log.Printf("SSL enabled for %s: serving HTTPS (ports 80/443)", settings.CustomDomain)
		if err := certmagic.HTTPS([]string{settings.CustomDomain}, router); err != nil {
			log.Fatalf("certmagic HTTPS: %v", err)
		}
		return
	}

	log.Printf("listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, router); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

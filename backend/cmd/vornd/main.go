// Command vornd is the Vorn Media Server backend daemon.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/caddyserver/certmagic"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/backup"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/config"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/httpapi"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/logging"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/migrate"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/notify"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/sysstats"
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

	// -envfile exists for platforms with no native "load an env file into
	// this service" mechanism of their own (systemd has EnvironmentFile=,
	// Docker Compose has env_file:, but a native Windows service has
	// neither) -- see install.ps1.
	envFile := flag.String("envfile", "", "path to a KEY=VALUE env file to load before reading configuration")
	flag.Parse()
	if *envFile != "" {
		if err := config.LoadEnvFile(*envFile); err != nil {
			log.Fatalf("loading -envfile: %v", err)
		}
	}

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

	scanSvc, err := scanner.NewService(st, queue, cfg.ArtworkCacheDir)
	if err != nil {
		log.Fatalf("starting scanner service: %v", err)
	}

	// TMDb/TVDb/Fanart.tv/OMDb/OpenSubtitles/torrent/NZB (and the
	// acquisition service derived from the last two) are all built --  and
	// rebuilt -- by httpapi.Server.reconfigure, not here, so an Admin >
	// Integrations credential save or a torrent/NZB enable toggle takes
	// effect immediately with no restart. metadataSvc itself is still
	// constructed once since it's never nil, only its internal providers
	// change; MusicBrainz/Open Library need no credentials at all, so both
	// are always attached here too, unconditionally (whether they're
	// actually *used* is decided fresh from IntegrationSettings on every
	// sync run instead -- see metadata.Service.run).
	metadataSvc := metadata.NewService(st)
	metadataSvc.WithMusicProvider(metadata.NewMusicBrainzProvider())
	metadataSvc.WithAudiobookProvider(metadata.NewOpenLibraryProvider())

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

	// Debrid (Real-Debrid/TorBox) has no listening port or background
	// resources to gate behind an enable flag; it's a no-op until the admin
	// configures at least one account.
	debridSvc := debrid.NewService(st)

	// Webhook notifications have no listening port or background resources
	// to gate behind an enable flag either; it's a no-op until an admin
	// configures a URL in Admin > Notifications.
	notifySvc := notify.NewService(st)

	if update.IsDockerized() {
		log.Print("running under Docker: self-update is a no-op (rebuild/pull the image instead)")
	}
	updateSvc := update.NewService(cfg.GitHubRepo, version.Version)

	// Prefer /media (Docker's library bind mount, see deploy/docker-compose.yml)
	// for disk usage since that's the filesystem a media server admin
	// actually cares about running out of space on; native installs won't
	// have it, so fall back to the root filesystem.
	diskStatsPath := "/"
	if info, err := os.Stat("/media"); err == nil && info.IsDir() {
		diskStatsPath = "/media"
	}
	sysStatsSampler := sysstats.NewSampler(diskStatsPath)

	// Disabled by default (see store.GetBackupSettings' zero value) -- the
	// scheduler itself always runs, re-checking BackupSettings every 15
	// minutes, so toggling it on/off or changing the interval from Admin >
	// Backups takes effect without a restart.
	go backup.NewScheduler(cfg.PostgresDSN, cfg.BackupDir, st).Run(context.Background())

	// Prowlarr sync (mirrors indexers configured inside a Prowlarr instance
	// into Vorn's own torrent/NZB indexer tables) is wired up inside
	// httpapi.Server.reconfigure now, not here -- it restarts itself
	// whenever torrent/NZB actually change, so toggling either via Admin >
	// Integrations re-points it at the fresh instance with no restart.
	router := httpapi.NewRouter(httpapi.Deps{
		Store:        st,
		Config:       cfg,
		PostgresDSN:  cfg.PostgresDSN,
		BackupDir:    cfg.BackupDir,
		Scanner:      scanSvc,
		Metadata:     metadataSvc,
		TranscodeMgr: transcodeMgr,
		Debrid:       debridSvc,
		Notify:       notifySvc,
		Update:       updateSvc,
		LogBuffer:    logBuffer,
		SysStats:     sysStatsSampler,
		CORSOrigin:   cfg.CORSOrigin,
		DevMode:      cfg.DevMode,
	})

	// Automatically backfills cast/crew/similar-titles metadata as content
	// is added, instead of requiring an admin to remember to trigger
	// Admin > Libraries > Sync Metadata by hand -- always running (same
	// "just re-check on a timer" shape as the backup scheduler above).
	// Started only now, after NewRouter (whose NewServer call runs an
	// initial reconfigure() synchronously) so metadataSvc's TMDb provider
	// is already populated by the scheduler's very first, startup tick --
	// starting it any earlier would race that initial reconfigure and skip
	// silently on the immediate startup tick.
	go metadata.NewScheduler(metadataSvc).Run(context.Background())

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

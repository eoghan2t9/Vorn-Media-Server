// Command vornd is the Vorn Media Server backend daemon.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/config"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/httpapi"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/migrate"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
)

func main() {
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

	router := httpapi.NewRouter(httpapi.Deps{
		Store:        st,
		Scanner:      scanSvc,
		Metadata:     metadataSvc,
		TranscodeMgr: transcodeMgr,
		Torrent:      torrentSvc,
		CORSOrigin:   cfg.CORSOrigin,
		DevMode:      cfg.DevMode,
	})

	log.Printf("listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, router); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

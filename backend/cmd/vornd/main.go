// Command vornd is the Vorn Media Server backend daemon.
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/config"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/httpapi"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/migrate"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
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

	router := httpapi.NewRouter(st, scanSvc, metadataSvc, cfg.CORSOrigin, cfg.DevMode)

	log.Printf("listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, router); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

// Command vornd is the Vorn Media Server backend daemon.
package main

import (
	"log"
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/config"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/httpapi"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/migrate"
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

	router := httpapi.NewRouter(st, cfg.CORSOrigin)

	log.Printf("listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, router); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

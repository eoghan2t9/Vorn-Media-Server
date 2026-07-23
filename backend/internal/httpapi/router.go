// Package httpapi wires Vorn's HTTP routes.
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

var startedAt = time.Now()

// NewRouter returns the root HTTP handler for the Vorn backend.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	return mux
}

type healthResponse struct {
	Status  string `json:"status"`
	Uptime  string `json:"uptime"`
	Version string `json:"version"`
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:  "ok",
		Uptime:  time.Since(startedAt).Round(time.Second).String(),
		Version: "0.0.0-dev",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

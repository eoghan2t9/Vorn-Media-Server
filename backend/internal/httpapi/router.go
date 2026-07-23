// Package httpapi wires Vorn's HTTP routes.
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/version"
)

var startedAt = time.Now()

type healthResponse struct {
	Status  string `json:"status"`
	Uptime  string `json:"uptime"`
	Version string `json:"version"`
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:  "ok",
		Uptime:  time.Since(startedAt).Round(time.Second).String(),
		Version: version.Version,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

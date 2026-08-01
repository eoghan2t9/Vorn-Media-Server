package httpapi

import (
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// handleListUpgrades returns recent quality-upgrade events for the admin
// dashboard, newest first.
func (s *Server) handleListUpgrades(w http.ResponseWriter, r *http.Request) {
	upgrades, err := s.store.ListUpgrades(50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing upgrade history: "+err.Error())
		return
	}
	if upgrades == nil {
		upgrades = []*store.AcquisitionUpgrade{} // JSON [] not null
	}
	writeJSON(w, http.StatusOK, upgrades)
}

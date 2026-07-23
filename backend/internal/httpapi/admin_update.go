package httpapi

import (
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/update"
)

const updateNotApplicableUnderDocker = "self-update is not applicable when running under Docker: rebuild/pull the image instead"

type updateCheckResponse struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Applied         bool   `json:"applied"`
	Dockerized      bool   `json:"dockerized"`
}

func toUpdateCheckResponse(r *update.CheckResult) updateCheckResponse {
	return updateCheckResponse{
		CurrentVersion:  r.CurrentVersion,
		LatestVersion:   r.LatestVersion,
		UpdateAvailable: r.UpdateAvailable,
		Applied:         r.Applied,
	}
}

// handleCheckForUpdate never refuses under Docker (unlike Apply): a
// dockerized install can still usefully learn "a newer release exists" even
// though applying it in place doesn't make sense there. The response's
// dockerized field lets the admin UI grey out the apply button accordingly.
func (s *Server) handleCheckForUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updateSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "self-update is not configured")
		return
	}
	result, err := s.updateSvc.Check(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	resp := toUpdateCheckResponse(result)
	resp.Dockerized = update.IsDockerized()
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updateSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "self-update is not configured")
		return
	}
	if update.IsDockerized() {
		writeError(w, http.StatusConflict, updateNotApplicableUnderDocker)
		return
	}
	result, err := s.updateSvc.Apply(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toUpdateCheckResponse(result))
}

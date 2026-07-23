package httpapi

import "net/http"

const subtitlesServiceUnavailable = "subtitle integration is not configured (set VORN_OPENSUBTITLES_API_KEY and VORN_OPENSUBTITLES_USERNAME)"

// handleGetSubtitles serves a WebVTT subtitle track for a media item,
// suitable for direct use as an HTML5 <track src="..."> URL. It fetches
// (and caches) on first request per item.itemForPlayback's Path (nil for a
// remote/debrid item, since moviehash needs to read real file bytes -- a
// known limitation of this pass) and requires a real local file.
func (s *Server) handleGetSubtitles(w http.ResponseWriter, r *http.Request) {
	if s.subtitlesSvc == nil {
		writeError(w, http.StatusServiceUnavailable, subtitlesServiceUnavailable)
		return
	}

	item := s.itemForPlayback(w, r, r.PathValue("id"))
	if item == nil {
		return
	}
	if isRemoteURL(*item.Path) {
		writeError(w, http.StatusUnprocessableEntity, "subtitle lookup isn't supported for remote (debrid) content")
		return
	}

	language := r.URL.Query().Get("language")
	if language == "" {
		language = "en"
	}

	vttPath, err := s.subtitlesSvc.Fetch(r.Context(), *item.Path, language)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/vtt")
	http.ServeFile(w, r, vttPath)
}

type subtitlesQuotaResponse struct {
	Remaining int    `json:"remaining"`
	ResetTime string `json:"resetTime,omitempty"`
}

// handleSubtitlesQuota surfaces OpenSubtitles' last known daily download
// quota in the admin UI. Remaining is -1 until the account has actually
// logged in or downloaded at least once, since OpenSubtitles only reports
// it then.
func (s *Server) handleSubtitlesQuota(w http.ResponseWriter, r *http.Request) {
	if s.subtitlesSvc == nil {
		writeJSON(w, http.StatusOK, subtitlesQuotaResponse{Remaining: -1})
		return
	}
	q := s.subtitlesSvc.Quota()
	writeJSON(w, http.StatusOK, subtitlesQuotaResponse{Remaining: q.Remaining, ResetTime: q.ResetTime})
}

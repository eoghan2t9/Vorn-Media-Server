package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
	"github.com/google/uuid"
)

// sessionFileWaitTimeout/sessionFileWaitInterval bound handleSessionFile's
// wait for a requested file to actually exist on disk -- StartSession
// returns as soon as ffmpeg has been launched, not once it's produced
// anything, so the very first playlist request (and occasionally a segment
// request right as a new one is being written) can race ffmpeg's output and
// hit a plain 404 with nothing wrong at all. Without this, that 404 was
// fatal to hls.js, which tore the whole session down and started a brand
// new one via playItem -- repeatedly, before ffmpeg ever got a chance to
// catch up, so playback never started at all.
const sessionFileWaitTimeout = 8 * time.Second
const sessionFileWaitInterval = 100 * time.Millisecond

type capabilitiesResponse struct {
	Backends []string `json:"backends"`
}

func (s *Server) handleTranscodeCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.transcodeMgr == nil {
		writeJSON(w, http.StatusOK, capabilitiesResponse{Backends: nil})
		return
	}
	names := make([]string, 0)
	for _, b := range s.transcodeMgr.Capabilities() {
		names = append(names, b.Name)
	}
	writeJSON(w, http.StatusOK, capabilitiesResponse{Backends: names})
}

type audioTrackResponse struct {
	Index    int    `json:"index"`
	Codec    string `json:"codec"`
	Language string `json:"language,omitempty"`
	Channels int    `json:"channels,omitempty"`
	Title    string `json:"title,omitempty"`
}

type chapterResponse struct {
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
	Title        string  `json:"title,omitempty"`
}

type playResponse struct {
	Mode        string `json:"mode"` // "direct" | "progressive" | "transcode" | "acquiring"
	DirectURL   string `json:"directUrl,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	PlaylistURL string `json:"playlistUrl,omitempty"`
	// ProgressiveURL is set for Mode == "progressive" -- a ModeRemux session
	// (see transcode.ModeRemux), served as a single growing MP4 file rather
	// than an HLS playlist. The frontend plays it exactly like DirectURL
	// (plain video.src, no hls.js) -- see WatchPage's attach().
	ProgressiveURL    string               `json:"progressiveUrl,omitempty"`
	AcquisitionStatus string               `json:"acquisitionStatus,omitempty"` // "searching" | "acquiring" | "error", only set when Mode == "acquiring"
	AcquisitionError  string               `json:"acquisitionError,omitempty"`
	DurationSeconds   float64              `json:"durationSeconds,omitempty"`
	AudioTracks       []audioTrackResponse `json:"audioTracks,omitempty"`
	Chapters          []chapterResponse    `json:"chapters,omitempty"`
}

// playRequest is an optional JSON body on POST /api/items/{id}/play --
// clients report codecs their player can decode natively (typically probed
// via video.canPlayType()) so handlePlayItem's transcode.Decide call can
// grant direct play beyond the conservative global baseline, and optionally
// which audio track to play (AudioTrackInfo.Index from a previous response's
// AudioTracks) when the viewer picked a non-default one. An absent or empty
// body decodes to the zero value, i.e. baseline-only behavior with the
// default audio track, so older/other clients that don't send this need no
// special-casing.
type playRequest struct {
	VideoCodecs     []string `json:"videoCodecs"`
	AudioCodecs     []string `json:"audioCodecs"`
	AudioTrackIndex *int     `json:"audioTrackIndex"`
}

// decodePlayRequest reads an optional playRequest JSON body without treating
// a missing/empty body as an error -- most callers (or a client
// mid-rollout) won't send one yet.
func decodePlayRequest(r *http.Request) playRequest {
	var req playRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	return req
}

func mediaInfoToPlayResponseFields(info *transcode.MediaInfo) (tracks []audioTrackResponse, chapters []chapterResponse) {
	for _, t := range info.AudioTracks {
		tracks = append(tracks, audioTrackResponse{Index: t.Index, Codec: t.Codec, Language: t.Language, Channels: t.Channels, Title: t.Title})
	}
	for _, c := range info.Chapters {
		chapters = append(chapters, chapterResponse{StartSeconds: c.StartSeconds, EndSeconds: c.EndSeconds, Title: c.Title})
	}
	return tracks, chapters
}

// loadItemForPlayback loads a media item and checks the caller has access
// to its library (or writes an error response and returns nil). Whether a
// playable file/stream actually exists yet is left to the caller: a
// placeholder item legitimately has no path, and handlePlayItem needs to
// treat that as "kick off acquisition" rather than an error the way
// handleDirectStream still must.
func (s *Server) loadItemForPlayback(w http.ResponseWriter, r *http.Request, id string) *store.MediaItem {
	user := userFromContext(r.Context())
	item, err := s.store.GetMediaItem(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading item")
		return nil
	}
	ok, err := s.canAccessLibrary(user, item.LibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking permissions")
		return nil
	}
	if !ok {
		writeError(w, http.StatusForbidden, "no access to this item")
		return nil
	}
	return item
}

// itemForPlayback is loadItemForPlayback plus the strict "must already have
// a file/stream" guard every caller except handlePlayItem still needs
// (client-API-compat playback info, subtitle extraction, direct file
// serving -- none of those know how to wait on an in-progress acquisition).
func (s *Server) itemForPlayback(w http.ResponseWriter, r *http.Request, id string) *store.MediaItem {
	item := s.loadItemForPlayback(w, r, id)
	if item == nil {
		return nil
	}
	if item.Path == nil || *item.Path == "" {
		writeError(w, http.StatusUnprocessableEntity, "item has no playable file")
		return nil
	}
	return item
}

func (s *Server) handlePlayItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req := decodePlayRequest(r)
	caps := transcode.ClientCapabilities{VideoCodecs: req.VideoCodecs, AudioCodecs: req.AudioCodecs}
	item := s.loadItemForPlayback(w, r, id)
	if item == nil {
		return
	}

	// A reacquire triggered by a dead-link detection below leaves the old
	// (dead) path in place while it runs -- check status before path so a
	// second play click during that window reports progress instead of
	// re-probing a link already known to be dead.
	if item.AcquisitionStatus == "searching" || item.AcquisitionStatus == "acquiring" {
		s.handleAcquiringPlay(w, r, item)
		return
	}

	if item.Path == nil || *item.Path == "" {
		s.handleAcquiringPlay(w, r, item)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	info, err := transcode.Probe(ctx, *item.Path)
	if err != nil {
		// A debrid-backed item's path is a provider CDN URL, not a local
		// file -- those commonly expire between viewing sessions, so a
		// probe failure there most likely means the link died, not that the
		// file is actually gone. Kick off the same search->score->resolve
		// pipeline a fresh acquisition uses to replace it, and report
		// "acquiring" exactly like a first-time acquisition would --
		// WatchPage's existing acquiring-overlay/poll loop handles this
		// automatically, no frontend changes needed. A local scanned file
		// failing to probe has no such replacement path, so that still
		// falls through to the plain error below.
		//
		// Deliberately not restricted to AcquisitionStatus == "owned": by
		// this point in the function "searching"/"acquiring" have already
		// been handled above, so the only other states reaching here are
		// "owned" (the common case) and "error" (e.g. an earlier attempt
		// left behind by ResetStuckAcquisitions, or a real prior failure) --
		// both have a real, present path that just needs replacing, and
		// Reacquire's own startAcquire already treats "error" exactly like
		// "placeholder" (kicks off a fresh attempt) internally, so gating
		// this on "owned" only served to strand an "error" item with a dead
		// link in a dead end: not empty enough to trigger a fresh Acquire,
		// not "owned" enough to trigger a Reacquire.
		if s.acquisition.Load() != nil && isRemoteURL(*item.Path) {
			if rErr := s.acquisition.Load().Reacquire(r.Context(), item.ID); rErr == nil {
				writeJSON(w, http.StatusAccepted, playResponse{Mode: "acquiring", AcquisitionStatus: "searching"})
				return
			}
		}
		writeError(w, http.StatusUnprocessableEntity, "probing media file")
		return
	}

	mode := transcode.Decide(info, caps)
	audioTrackIndex := -1
	if req.AudioTrackIndex != nil {
		audioTrackIndex = *req.AudioTrackIndex
		// Picking a specific audio track requires ffmpeg to select and
		// (re)mux it -- a direct-play redirect bypasses ffmpeg entirely, so
		// a request for a non-default track can never stay in ModeDirect
		// even if the source video codec alone would otherwise qualify.
		if mode == transcode.ModeDirect {
			mode = transcode.ModeRemux
		}
	}
	audioTracks, chapters := mediaInfoToPlayResponseFields(info)

	if mode == transcode.ModeDirect {
		writeJSON(w, http.StatusOK, playResponse{
			Mode:            "direct",
			DirectURL:       "/api/stream/direct/" + item.ID,
			DurationSeconds: info.DurationSeconds,
			AudioTracks:     audioTracks,
			Chapters:        chapters,
		})
		return
	}

	if s.transcodeMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "transcoding is not available (ffmpeg not detected)")
		return
	}

	isRemux := mode == transcode.ModeRemux
	sessionID := uuid.NewString()
	sess, err := s.transcodeMgr.StartSession(context.Background(), sessionID, *item.Path, isRemux, audioTrackIndex)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "starting transcode session")
		return
	}

	// ModeRemux is served as a single progressive MP4 (see
	// Session.ProgressiveOutputPath/streamGrowingFile) rather than an HLS
	// playlist -- the frontend needs to know which, since it plays a
	// progressive URL exactly like direct play (plain video.src, no
	// hls.js) instead of attaching hls.js to a playlist.
	if isRemux {
		writeJSON(w, http.StatusAccepted, playResponse{
			Mode:            "progressive",
			SessionID:       sess.ID,
			ProgressiveURL:  "/api/stream/session/" + sess.ID + "/output.mp4",
			DurationSeconds: info.DurationSeconds,
			AudioTracks:     audioTracks,
			Chapters:        chapters,
		})
		return
	}
	writeJSON(w, http.StatusAccepted, playResponse{
		Mode:            "transcode",
		SessionID:       sess.ID,
		PlaylistURL:     "/api/stream/session/" + sess.ID + "/playlist.m3u8",
		DurationSeconds: info.DurationSeconds,
		AudioTracks:     audioTracks,
		Chapters:        chapters,
	})
}

// handleAcquiringPlay is handlePlayItem's branch for an item with no
// path/stream yet: a freshly-opened placeholder starts acquisition, one
// already in flight just reports its current status, and a previously
// failed attempt is reported so the frontend can offer Retry (which lands
// back here and re-tries since Acquire treats 'error' the same as
// 'placeholder').
func (s *Server) handleAcquiringPlay(w http.ResponseWriter, r *http.Request, item *store.MediaItem) {
	switch item.AcquisitionStatus {
	case "searching", "acquiring":
		writeJSON(w, http.StatusAccepted, playResponse{Mode: "acquiring", AcquisitionStatus: item.AcquisitionStatus})
	case "placeholder", "error":
		if s.acquisition.Load() == nil {
			writeError(w, http.StatusServiceUnavailable, "on-demand acquisition is not configured")
			return
		}
		if err := s.acquisition.Load().Acquire(r.Context(), item.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "starting acquisition")
			return
		}
		writeJSON(w, http.StatusAccepted, playResponse{Mode: "acquiring", AcquisitionStatus: "searching"})
	default:
		// "owned" with no path is an inconsistent state that shouldn't
		// happen -- surface the original hard failure rather than silently
		// starting acquisition on an item that was never a placeholder.
		writeError(w, http.StatusUnprocessableEntity, "item has no playable file")
	}
}

func (s *Server) handleDirectStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item := s.itemForPlayback(w, r, id)
	if item == nil {
		return
	}
	// Debrid-backed items store a provider CDN URL as their path rather than
	// a local file: redirect the player straight to it instead of trying to
	// serve it off local disk (there is none -- that's the point of
	// direct-stream-from-debrid).
	if isRemoteURL(*item.Path) {
		http.Redirect(w, r, *item.Path, http.StatusFound)
		return
	}
	http.ServeFile(w, r, *item.Path)
}

func isRemoteURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

// handleSessionFile serves playlist/segment files out of a transcode
// session's output directory. filepath.Base strips any directory
// components from the requested name so a path-traversal attempt (e.g.
// "../../etc/passwd") can't escape the session directory.
func (s *Server) handleSessionFile(w http.ResponseWriter, r *http.Request) {
	if s.transcodeMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "transcoding is not available")
		return
	}
	sessionID := r.PathValue("sessionId")
	file := filepath.Base(r.PathValue("file"))

	sess, ok := s.transcodeMgr.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	fullPath := filepath.Join(sess.OutputDir, file)
	if !strings.HasPrefix(fullPath, sess.OutputDir) {
		writeError(w, http.StatusBadRequest, "invalid file")
		return
	}

	if !waitForSessionFile(r.Context(), sess, fullPath) {
		writeError(w, http.StatusNotFound, "file not ready")
		return
	}

	// Deliberately not http.ServeFile/http.ServeContent -- both implement
	// conditional GET (ETag/Last-Modified) automatically, and once the
	// browser's HTTP cache has one response for a given segment URL, it
	// attaches If-Modified-Since on the next identical request. That's fine
	// for ordinary static files, but hls.js has no reason to ever re-request
	// the same fragment URL unless its first attempt failed to parse -- a
	// 304's empty body IS a parse failure, so it retries the same URL,
	// gets another 304, and never gets any actual bytes: an infinite loop
	// that looks exactly like "playback stalls, needs replaying constantly"
	// from the user's side, with the segment fetch itself always
	// "succeeding" (304 is not an error). No Last-Modified/ETag at all means
	// there is nothing for a conditional request to validate against, so
	// every fetch gets the real 200 + full body.
	w.Header().Set("Cache-Control", "no-store")

	if strings.HasSuffix(file, ".mp4") {
		// ModeRemux's progressive output -- ffmpeg is likely still writing
		// this file, so it's followed in place rather than read once. See
		// streamGrowingFile.
		w.Header().Set("Content-Type", "video/mp4")
		streamGrowingFile(w, r, sess, fullPath)
		return
	}

	f, err := os.Open(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not ready")
		return
	}
	defer f.Close()

	if strings.HasSuffix(file, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else if strings.HasSuffix(file, ".ts") {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	if info, err := f.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	// Error is expectedly common and not worth logging -- a client
	// abandoning a fragment mid-download (seeking away, navigating off the
	// page) just breaks the pipe, same as any other write to an
	// http.ResponseWriter for a client that's gone.
	_, _ = io.Copy(w, f)
}

// streamGrowingFile serves fullPath as ffmpeg writes it, following its
// growth in place instead of stopping at EOF -- ModeRemux's progressive MP4
// output is deliberately played while still being produced (see
// buildFFmpegArgs's frag_keyframe+empty_moov). Unlike the original version,
// Content-Range headers and 206 responses are supported for seeking into
// already-written content: if the client sends a Range header and the
// requested bytes exist on disk, they are served with a 206 Partial Content
// response. If the requested range is beyond what's been written so far, the
// response falls back to serving whatever is available (the browser retries
// as more content arrives). No Content-Length is set since the total size
// isn't known upfront.
func streamGrowingFile(w http.ResponseWriter, r *http.Request, sess *transcode.Session, fullPath string) {
	f, err := os.Open(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not ready")
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stat file")
		return
	}

	w.Header().Set("Accept-Ranges", "bytes")

	// Parse Range header for seeking support -- respond with 206 Partial
	// Content if the requested range is within already-written bytes, or
	// serve the entire available content with 200 OK if the range extends
	// beyond what's been written so far.
	start := int64(0)
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		if parsedStart, parsedEnd, ok := parseByteRange(rangeHeader, fi.Size()); ok && parsedStart < fi.Size() {
			start = parsedStart
			end := parsedEnd
			if end > fi.Size() {
				end = fi.Size()
			}
			if _, err := f.Seek(start, io.SeekStart); err != nil {
				// If seeking fails, fall through to serving from the start.
				start = 0
			} else {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/*", start, end-1))
				w.WriteHeader(http.StatusPartialContent)
			}
		}
	}
	if start == 0 && w.Header().Get("Content-Range") == "" {
		w.WriteHeader(http.StatusOK)
	}

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 256*1024)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return // client disconnected
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			status := sess.Status()
			if status != transcode.StatusRunning && status != transcode.StatusStarting {
				return // ffmpeg is done producing output and we've drained all of it
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(sessionFileWaitInterval):
			}
			continue
		}
		if readErr != nil {
			return
		}
	}
}

// parseByteRange parses an HTTP Range header value ("bytes=start-end") and
// returns the start and end positions. If end is 0, the range is open-ended
// ("bytes=start-"). Adapted for progressive-mode growing files where the
// total size is not known.
func parseByteRange(rangeVal string, fileSize int64) (start, end int64, ok bool) {
	if !strings.HasPrefix(rangeVal, "bytes=") {
		return 0, 0, false
	}
	rangeVal = strings.TrimPrefix(rangeVal, "bytes=")
	parts := strings.SplitN(rangeVal, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}
	endStr := strings.TrimSpace(parts[1])
	if endStr == "" {
		// Open-ended: serve from start to end of what's available.
		return start, fileSize, true
	}
	end, err = strconv.ParseInt(endStr, 10, 64)
	if err != nil || end <= start {
		return 0, 0, false
	}
	return start, end + 1, true // end is inclusive in HTTP Range spec, convert to exclusive
}

// waitForSessionFile polls for fullPath to appear, bounded by
// sessionFileWaitTimeout -- see the const's doc comment for why this is
// needed at all. Gives up early (no point waiting out the full timeout) if
// the session has already failed, since it's not going to produce the file
// no matter how long this waits, or if the request itself is cancelled
// (the client gave up).
func waitForSessionFile(ctx context.Context, sess *transcode.Session, fullPath string) bool {
	deadline := time.Now().Add(sessionFileWaitTimeout)
	for {
		if _, err := os.Stat(fullPath); err == nil {
			return true
		}
		if sess.Status() == transcode.StatusFailed {
			return false
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(sessionFileWaitInterval):
		}
	}
}

func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	if s.transcodeMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "transcoding is not available")
		return
	}
	sessionID := r.PathValue("sessionId")
	if err := s.transcodeMgr.Stop(sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "stopping session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

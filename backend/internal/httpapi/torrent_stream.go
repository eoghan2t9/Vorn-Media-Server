package httpapi

import (
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

// handleTorrentStream serves the largest video file in an active,
// still-downloading torrent directly to a player -- see
// torrent.Service.StreamFile's doc comment for how the underlying blocking
// read works. This is deliberately not routed through media_items/playItem
// (no probe/transcode negotiation): only browser-natively-playable sources
// work here, matching handlePlayItem's ModeDirect case. A torrent needing
// transcoding remains playable only after it fully completes and gets
// promoted through the normal library pipeline.
func (s *Server) handleTorrentStream(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	id := r.PathValue("id")

	reader, size, name, err := s.torrentSvc.Load().StreamFile(r.Context(), id)
	if err != nil {
		if errors.Is(err, torrent.ErrTorrentNotStreamable) {
			writeError(w, http.StatusServiceUnavailable, "torrent has no streamable video file yet")
			return
		}
		s.writeStoreErr(w, err, "loading torrent")
		return
	}
	defer reader.Close()

	start, end := int64(0), size
	status := http.StatusOK
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		if parsedStart, parsedEnd, ok := parseByteRange(rangeHeader, size); ok && parsedStart < size {
			start, end = parsedStart, parsedEnd
			if end > size {
				end = size
			}
			status = http.StatusPartialContent
		}
	}
	if start > 0 {
		if _, err := reader.Seek(start, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, "seeking into torrent file")
			return
		}
	}

	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(end-start, 10))
	if status == http.StatusPartialContent {
		w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end-1, 10)+"/"+strconv.FormatInt(size, 10))
	}
	w.WriteHeader(status)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 256*1024)
	remaining := end - start
	for remaining > 0 {
		n := int64(len(buf))
		if remaining < n {
			n = remaining
		}
		read, readErr := reader.Read(buf[:n])
		if read > 0 {
			if _, writeErr := w.Write(buf[:read]); writeErr != nil {
				return // client disconnected
			}
			if flusher != nil {
				flusher.Flush()
			}
			remaining -= int64(read)
		}
		if readErr != nil {
			if readErr != io.EOF {
				log.Printf("httpapi: streaming torrent %s: %v", id, readErr)
			}
			return
		}
	}
}

package httpapi

import (
	"net/http"

	"github.com/gorilla/websocket"
)

// logsUpgrader allows the CORS-configured frontend origin (which may differ
// from the backend's own, e.g. the Vite dev server) to open a WebSocket:
// gorilla's default CheckOrigin only allows same-origin, which would reject
// every real deployment of Vorn's own admin UI.
var logsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleAdminLogsStream streams the server's own log output live over a
// WebSocket: on connect it replays whatever's in the ring buffer (recent
// scrollback), then forwards every new line as it's logged. There's no log
// file involved at all -- this works the same whether Vorn is running under
// Docker, systemd, or a bare terminal.
func (s *Server) handleAdminLogsStream(w http.ResponseWriter, r *http.Request) {
	if s.logBuffer == nil {
		writeError(w, http.StatusServiceUnavailable, "log buffer not configured")
		return
	}

	conn, err := logsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for _, line := range s.logBuffer.Recent() {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(line)); err != nil {
			return
		}
	}

	lines, unsubscribe := s.logBuffer.Subscribe()
	defer unsubscribe()

	// A read loop is required alongside the write loop purely so gorilla
	// notices the client closing the connection (e.g. navigating away);
	// nothing the client would actually send is expected or used.
	clientGone := make(chan struct{})
	go func() {
		defer close(clientGone)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(line)); err != nil {
				return
			}
		case <-clientGone:
			return
		}
	}
}

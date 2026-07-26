package httpapi

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// jfSocketUpgrader mirrors logsUpgrader's CheckOrigin relaxation: official
// Jellyfin apps (mobile/TV) open this cross-origin same as the admin log
// stream does from the frontend dev server.
var jfSocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type jfSocketMessage struct {
	MessageType string `json:"MessageType"`
	Data        any    `json:"Data,omitempty"`
}

// handleJfSocket implements just enough of Jellyfin's WebSocket protocol
// (GET /socket, upgraded) for official apps to stop waiting on it: confirmed
// behavior is that the native apps open this immediately after
// AuthenticateByName and hold their own post-login loading spinner open
// until the upgrade succeeds, even though Vorn has nothing real-time to push
// over it (no live transcode sessions, no SyncPlay -- see the jellyfin
// package doc comment for what's actually implemented). ForceKeepAlive tells
// the client how often to expect a message before it should reconnect;
// KeepAlive is the client's own periodic ping, which the protocol expects
// echoed back.
func (s *Server) handleJfSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := jfSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	const keepAliveInterval = 30 * time.Second
	if err := conn.WriteJSON(jfSocketMessage{MessageType: "ForceKeepAlive", Data: int(keepAliveInterval.Seconds())}); err != nil {
		return
	}

	clientGone := make(chan struct{})
	go func() {
		defer close(clientGone)
		for {
			var msg jfSocketMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.MessageType == "KeepAlive" {
				if err := conn.WriteJSON(jfSocketMessage{MessageType: "KeepAlive"}); err != nil {
					return
				}
			}
		}
	}()

	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := conn.WriteJSON(jfSocketMessage{MessageType: "ForceKeepAlive", Data: int(keepAliveInterval.Seconds())}); err != nil {
				return
			}
		case <-clientGone:
			return
		}
	}
}

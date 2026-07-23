package httpapi

import (
	"log"
	"net/http"
	"os"
	"time"
)

// handleRestartServer exits the process shortly after responding, relying
// on something outside the process to bring it back up: Docker's
// `restart: unless-stopped` policy (the default deploy/docker-compose.yml)
// restarts on any exit, including a clean os.Exit(0), unless the container
// was explicitly stopped from the host -- which is also why there's no
// equivalent "shut down and stay down" endpoint here: doing that from
// inside the container would need the host's Docker socket mounted in,
// handing the container root-equivalent control over the whole Docker
// host, a bigger privilege trade than a restart button justifies. Native
// (non-Docker) installs need their own process supervisor (e.g. a systemd
// unit with Restart=on-failure) for this to actually come back up; a bare
// `go run`/manual invocation will just stay down.
func (s *Server) handleRestartServer(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Restarting"})
	go func() {
		// Give the ResponseWriter time to flush before the process exits --
		// exiting synchronously in the handler risks the client never
		// seeing the response at all.
		time.Sleep(300 * time.Millisecond)
		log.Print("admin requested a restart: exiting (Docker's restart policy, or a process supervisor on non-Docker installs, is what actually brings it back up)")
		os.Exit(0)
	}()
}

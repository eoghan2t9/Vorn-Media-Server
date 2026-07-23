package transcode

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type SessionStatus string

const (
	StatusStarting SessionStatus = "starting"
	StatusRunning  SessionStatus = "running"
	StatusStopped  SessionStatus = "stopped"
	StatusFailed   SessionStatus = "failed"
)

type Session struct {
	ID         string
	SourcePath string
	OutputDir  string
	Backend    string

	mu     sync.Mutex
	status SessionStatus
	cancel context.CancelFunc
}

func (s *Session) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Session) setStatus(v SessionStatus) {
	s.mu.Lock()
	s.status = v
	s.mu.Unlock()
}

func (s *Session) PlaylistPath() string {
	return filepath.Join(s.OutputDir, "playlist.m3u8")
}

// Manager runs and tracks HLS transcode sessions, bounding how many run
// concurrently so playback and library scans share the host's CPU/GPU
// instead of one starving the other under load.
type Manager struct {
	outputRoot string
	backends   []Backend
	sem        chan struct{}

	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager(outputRoot string, backends []Backend, maxConcurrent int) *Manager {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Manager{
		outputRoot: outputRoot,
		backends:   backends,
		sem:        make(chan struct{}, maxConcurrent),
		sessions:   make(map[string]*Session),
	}
}

// Capabilities returns the hardware/software backends detected at startup.
func (m *Manager) Capabilities() []Backend {
	return m.backends
}

func (m *Manager) bestBackend() Backend {
	for _, b := range m.backends {
		if b.Name != "software" {
			return b
		}
	}
	if len(m.backends) > 0 {
		return m.backends[0]
	}
	return Backend{Name: "software", Encoder: "libx264"}
}

// StartSession launches an ffmpeg process that transcodes sourcePath into
// an HLS playlist + segments under a per-session directory, using the best
// available backend. It blocks only until the queue slot is acquired and
// the process has been started, not until transcoding finishes.
func (m *Manager) StartSession(ctx context.Context, id, sourcePath string) (*Session, error) {
	outDir := filepath.Join(m.outputRoot, id)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("transcode: creating output dir: %w", err)
	}

	backend := m.bestBackend()
	sessCtx, cancel := context.WithCancel(context.Background())
	sess := &Session{
		ID:         id,
		SourcePath: sourcePath,
		OutputDir:  outDir,
		Backend:    backend.Name,
		status:     StatusStarting,
		cancel:     cancel,
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	go m.run(sessCtx, sess, backend)
	return sess, nil
}

func (m *Manager) run(ctx context.Context, sess *Session, backend Backend) {
	select {
	case m.sem <- struct{}{}:
	case <-ctx.Done():
		sess.setStatus(StatusStopped)
		return
	}
	defer func() { <-m.sem }()

	args := []string{"-hide_banner", "-y", "-i", sess.SourcePath}
	args = append(args, backend.DeviceArgs...)
	args = append(args, backend.FilterArgs...)
	args = append(args,
		"-c:v", backend.Encoder,
		"-c:a", "aac",
		"-f", "hls",
		"-hls_time", "4",
		"-hls_playlist_type", "event",
		"-hls_segment_filename", filepath.Join(sess.OutputDir, "seg%05d.ts"),
		sess.PlaylistPath(),
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if err := cmd.Start(); err != nil {
		log.Printf("transcode: session %s failed to start: %v", sess.ID, err)
		sess.setStatus(StatusFailed)
		return
	}
	sess.setStatus(StatusRunning)

	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		log.Printf("transcode: session %s exited with error: %v", sess.ID, err)
		sess.setStatus(StatusFailed)
		return
	}
	sess.setStatus(StatusStopped)
}

func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	return sess, ok
}

// Stop kills the ffmpeg process (if still running) and removes its output
// directory.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if !ok {
		return nil
	}
	sess.cancel()
	return os.RemoveAll(sess.OutputDir)
}

// ClearFinished removes tracking and on-disk output for every session that
// is no longer running. A session that finishes on its own (the client just
// stops requesting playlist segments, say) rather than via an explicit Stop
// otherwise leaks both its map entry and its output directory forever; this
// is what the admin "clear cache" maintenance action calls.
func (m *Manager) ClearFinished() (cleared int, err error) {
	m.mu.Lock()
	var finished []*Session
	for id, sess := range m.sessions {
		if s := sess.Status(); s == StatusStopped || s == StatusFailed {
			finished = append(finished, sess)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, sess := range finished {
		if rmErr := os.RemoveAll(sess.OutputDir); rmErr != nil {
			if err == nil {
				err = rmErr
			}
			continue
		}
		cleared++
	}
	return cleared, err
}

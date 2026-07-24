// Package sysstats reports host-level CPU/memory/disk/network usage for the
// admin dashboard. The only environment this actually ships in is Linux
// (bare metal, or Docker -- which always runs Linux containers even when
// the Docker host itself is Windows or macOS, via a Linux VM), so the Linux
// implementation is the "real" one, backed by /proc. But this repo is also
// built and run natively on Windows/macOS dev machines, so every exported
// symbol here must at least compile and degrade gracefully (never panic,
// never fail to build) on those platforms too -- see sysstats_darwin.go and
// sysstats_windows.go. Each metric reports its own availability rather than
// one blanket flag, since e.g. macOS can report disk+memory but not CPU or
// network with the approach used here.
package sysstats

import (
	"sync"
	"time"
)

type Snapshot struct {
	CPUAvailable bool
	CPUPercent   float64

	MemAvailable  bool
	MemUsedBytes  uint64
	MemTotalBytes uint64

	DiskAvailable  bool
	DiskUsedBytes  uint64
	DiskTotalBytes uint64

	NetAvailable     bool
	NetRxBytesPerSec float64
	NetTxBytesPerSec float64
}

const sampleInterval = 2 * time.Second

// Sampler periodically re-samples system stats in the background so the
// HTTP handler serving them never blocks on I/O, or on the sample-interval
// window a rate calculation (CPU%, network throughput) needs between two
// readings taken a known time apart.
type Sampler struct {
	diskPath string
	platform *platformState

	mu     sync.RWMutex
	latest Snapshot
}

// NewSampler starts the background sampling loop immediately. diskPath is
// the filesystem to report disk usage for -- pass a bind-mounted path (e.g.
// Docker's /media) to get the underlying host filesystem's real capacity,
// not the container's own overlay filesystem.
func NewSampler(diskPath string) *Sampler {
	s := &Sampler{diskPath: diskPath, platform: newPlatformState()}
	go s.loop()
	return s
}

func (s *Sampler) loop() {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
	for range ticker.C {
		snap := s.platform.sample(s.diskPath)
		s.mu.Lock()
		s.latest = snap
		s.mu.Unlock()
	}
}

// Latest returns the most recently sampled snapshot -- the zero-value
// Snapshot{} (every *Available field false) for the first sampleInterval
// after startup, before the first background sample has completed.
func (s *Sampler) Latest() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

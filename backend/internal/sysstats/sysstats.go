// Package sysstats reports host-level CPU/memory/disk usage for the admin
// dashboard, reading Linux's /proc/stat and /proc/meminfo directly rather
// than pulling in a general system-stats dependency -- this only ever runs
// on Linux (bare metal, or inside Docker, which shares the host's /proc
// unless the container runtime specifically virtualizes it, which standard
// Docker doesn't), so hand-parsing two well-known files is simpler and more
// auditable than a cross-platform library for a two-file job.
package sysstats

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Snapshot struct {
	Available      bool // false if /proc isn't readable (e.g. a non-Linux dev machine)
	CPUPercent     float64
	MemUsedBytes   uint64
	MemTotalBytes  uint64
	DiskUsedBytes  uint64
	DiskTotalBytes uint64
}

const sampleInterval = 2 * time.Second

// Sampler periodically re-reads system stats in the background so the HTTP
// handler serving them never blocks on I/O, or on the ~sampleInterval
// window a CPU-percent calculation needs between two /proc/stat snapshots.
type Sampler struct {
	diskPath string

	mu     sync.RWMutex
	latest Snapshot

	prevIdle  uint64
	prevTotal uint64
}

// NewSampler starts the background sampling loop immediately. diskPath is
// the filesystem to report disk usage for -- pass a bind-mounted path (e.g.
// Docker's /media) to get the underlying host filesystem's real capacity,
// not the container's own overlay filesystem.
func NewSampler(diskPath string) *Sampler {
	s := &Sampler{diskPath: diskPath}
	s.prevIdle, s.prevTotal, _ = readCPUStat()
	go s.loop()
	return s
}

func (s *Sampler) loop() {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.sample()
	}
}

func (s *Sampler) sample() {
	var snap Snapshot

	if idle, total, err := readCPUStat(); err == nil {
		deltaIdle := float64(idle - s.prevIdle)
		deltaTotal := float64(total - s.prevTotal)
		if deltaTotal > 0 {
			snap.CPUPercent = (1 - deltaIdle/deltaTotal) * 100
		}
		s.prevIdle, s.prevTotal = idle, total
		snap.Available = true
	}

	if used, total, err := readMemInfo(); err == nil {
		snap.MemUsedBytes, snap.MemTotalBytes = used, total
		snap.Available = true
	}

	if used, total, err := readDiskUsage(s.diskPath); err == nil {
		snap.DiskUsedBytes, snap.DiskTotalBytes = used, total
	}

	s.mu.Lock()
	s.latest = snap
	s.mu.Unlock()
}

// Latest returns the most recently sampled snapshot -- possibly the
// zero-value Snapshot{} for the first sampleInterval after startup, before
// the first background sample has completed.
func (s *Sampler) Latest() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

// readCPUStat parses /proc/stat's aggregate "cpu" line (all cores summed):
// user nice system idle iowait irq softirq steal guest guest_nice, in
// USER_HZ jiffies since boot. Returns (idle+iowait) and the sum of all
// fields, so callers compute %busy from the delta between two calls a known
// interval apart -- a single snapshot alone can't yield a percentage.
func readCPUStat() (idle, total uint64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return parseCPUStat(f)
}

func parseCPUStat(r io.Reader) (idle, total uint64, err error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return 0, 0, fmt.Errorf("sysstats: /proc/stat is empty")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, fmt.Errorf("sysstats: unexpected /proc/stat format")
	}

	var vals [10]uint64
	for i := 1; i < len(fields) && i <= len(vals); i++ {
		vals[i-1], _ = strconv.ParseUint(fields[i], 10, 64)
	}
	idle = vals[3] + vals[4] // idle + iowait
	for _, v := range vals {
		total += v
	}
	return idle, total, nil
}

// readMemInfo parses /proc/meminfo for MemTotal and MemAvailable (the
// kernel's own estimate of memory usable without swapping -- a more honest
// "how much is actually free" signal than MemFree alone, which excludes
// reclaimable page cache and so under-reports what's really available).
func readMemInfo() (usedBytes, totalBytes uint64, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return parseMemInfo(f)
}

func parseMemInfo(r io.Reader) (usedBytes, totalBytes uint64, err error) {
	var totalKB, availableKB uint64
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			totalKB = parseMemInfoKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			availableKB = parseMemInfoKB(line)
		}
	}
	if totalKB == 0 {
		return 0, 0, fmt.Errorf("sysstats: MemTotal not found in /proc/meminfo")
	}
	return (totalKB - availableKB) * 1024, totalKB * 1024, nil
}

func parseMemInfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseUint(fields[1], 10, 64)
	return v
}

// readDiskUsage reports usage of the filesystem containing path.
func readDiskUsage(path string) (usedBytes, totalBytes uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	totalBytes = stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bfree * uint64(stat.Bsize)
	return totalBytes - freeBytes, totalBytes, nil
}

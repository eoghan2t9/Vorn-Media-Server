//go:build linux

package sysstats

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// platformState holds the previous sample needed to turn Linux's monotonic
// counters (CPU jiffies since boot, network bytes since interface init)
// into rates.
type platformState struct {
	prevCPUIdle, prevCPUTotal uint64
	prevNetRx, prevNetTx      uint64
	prevNetTime               time.Time
}

func newPlatformState() *platformState {
	idle, total, _ := readCPUStat()
	rx, tx, _ := readNetDev()
	return &platformState{
		prevCPUIdle:  idle,
		prevCPUTotal: total,
		prevNetRx:    rx,
		prevNetTx:    tx,
		prevNetTime:  time.Now(),
	}
}

func (p *platformState) sample(diskPath string) Snapshot {
	var snap Snapshot

	if idle, total, err := readCPUStat(); err == nil {
		deltaIdle := float64(idle - p.prevCPUIdle)
		deltaTotal := float64(total - p.prevCPUTotal)
		if deltaTotal > 0 {
			snap.CPUPercent = (1 - deltaIdle/deltaTotal) * 100
		}
		p.prevCPUIdle, p.prevCPUTotal = idle, total
		snap.CPUAvailable = true
	}

	if used, total, err := readMemInfo(); err == nil {
		snap.MemUsedBytes, snap.MemTotalBytes = used, total
		snap.MemAvailable = true
	}

	if used, total, err := readDiskUsage(diskPath); err == nil {
		snap.DiskUsedBytes, snap.DiskTotalBytes = used, total
		snap.DiskAvailable = true
	}

	if rx, tx, err := readNetDev(); err == nil {
		now := time.Now()
		elapsed := now.Sub(p.prevNetTime).Seconds()
		if elapsed > 0 {
			// Counters reset to 0 if an interface is torn down and recreated
			// (e.g. Docker recreating a bridge) -- treat a decrease as "no
			// data this tick" rather than reporting a nonsensical negative
			// rate from unsigned wraparound.
			if rx >= p.prevNetRx {
				snap.NetRxBytesPerSec = float64(rx-p.prevNetRx) / elapsed
			}
			if tx >= p.prevNetTx {
				snap.NetTxBytesPerSec = float64(tx-p.prevNetTx) / elapsed
			}
			snap.NetAvailable = true
		}
		p.prevNetRx, p.prevNetTx, p.prevNetTime = rx, tx, now
	}

	return snap
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

// readNetDev sums RX/TX bytes across every non-loopback interface from
// /proc/net/dev, so a multi-NIC host still gets one meaningful total rather
// than requiring the admin to know which interface name is "the" one.
func readNetDev() (rxBytes, txBytes uint64, err error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return parseNetDev(f)
}

func parseNetDev(r io.Reader) (rxBytes, txBytes uint64, err error) {
	scanner := bufio.NewScanner(r)
	lineNum := 0
	found := false
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue // two header lines
		}
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" || strings.HasPrefix(iface, "docker") || strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "br-") {
			// Loopback and Docker's own internal bridge/veth interfaces
			// aren't "network usage" from the admin's point of view -- they'd
			// double-count traffic that's really going out a physical/host
			// interface, or just be inter-container chatter.
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		rxBytes += rx
		txBytes += tx
		found = true
	}
	if !found {
		return 0, 0, fmt.Errorf("sysstats: no usable interfaces found in /proc/net/dev")
	}
	return rxBytes, txBytes, nil
}

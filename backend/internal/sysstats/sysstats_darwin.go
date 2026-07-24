//go:build darwin

package sysstats

import (
	"bufio"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// platformState is empty on Darwin -- neither of the two metrics
// implemented here (disk, memory) needs a delta between samples the way
// Linux's CPU%/network-rate calculations do.
type platformState struct{}

func newPlatformState() *platformState {
	return &platformState{}
}

// sample reports disk and memory usage on macOS. CPU% and network
// throughput are left unavailable here: getting either without cgo means
// parsing `top`'s or `netstat`'s text output, whose exact column layout has
// changed across macOS versions and isn't practical to verify without a
// real Mac to test against -- reporting nothing is more honest than
// shipping an unverified parser that might silently misread its input.
func (p *platformState) sample(diskPath string) Snapshot {
	var snap Snapshot

	if used, total, ok := readDiskUsage(diskPath); ok {
		snap.DiskUsedBytes, snap.DiskTotalBytes = used, total
		snap.DiskAvailable = true
	}

	if used, total, ok := readMemInfo(); ok {
		snap.MemUsedBytes, snap.MemTotalBytes = used, total
		snap.MemAvailable = true
	}

	return snap
}

func readDiskUsage(path string) (usedBytes, totalBytes uint64, ok bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, false
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	return total - free, total, true
}

var vmStatPageSizeRe = regexp.MustCompile(`page size of (\d+) bytes`)

// readMemInfo shells out to `sysctl`/`vm_stat` -- the same "call a stable
// system CLI and parse its output" approach this codebase already uses for
// ffprobe, and the simplest way to get real memory numbers on macOS without
// cgo (there's no /proc here, and the Mach APIs for this aren't reachable
// from pure Go). "Used" is approximated as everything that isn't a free
// page (active + inactive + wired + compressed), which is the same
// convention Activity Monitor's memory pressure view uses.
func readMemInfo() (usedBytes, totalBytes uint64, ok bool) {
	totalOut, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, 0, false
	}
	total, err := strconv.ParseUint(strings.TrimSpace(string(totalOut)), 10, 64)
	if err != nil || total == 0 {
		return 0, 0, false
	}

	vmOut, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, 0, false
	}

	pageSize := uint64(4096)
	if m := vmStatPageSizeRe.FindSubmatch(vmOut); len(m) == 2 {
		if v, err := strconv.ParseUint(string(m[1]), 10, 64); err == nil && v > 0 {
			pageSize = v
		}
	}

	var freePages uint64
	scanner := bufio.NewScanner(strings.NewReader(string(vmOut)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Pages free:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		v := strings.TrimSuffix(fields[2], ".")
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		freePages = n
	}

	freeBytes := freePages * pageSize
	if freeBytes > total {
		return 0, 0, false
	}
	return total - freeBytes, total, true
}

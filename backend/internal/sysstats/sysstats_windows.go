//go:build windows

package sysstats

import (
	"syscall"
	"unsafe"
)

var (
	modkernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = modkernel32.NewProc("GetSystemTimes")
	procGetDiskFreeSpaceExW  = modkernel32.NewProc("GetDiskFreeSpaceExW")
)

// platformState holds the previous CPU-time sample needed to turn
// GetSystemTimes' monotonic FILETIME counters into a percentage. Network
// throughput isn't implemented here -- the real Win32 API for it
// (IP Helper's GetIfTable2) is considerably more involved than the three
// calls used below, and isn't practical to get right without a real
// Windows machine to verify against.
type platformState struct {
	prevIdle, prevTotal uint64
}

func newPlatformState() *platformState {
	idle, kernel, user, ok := readSystemTimes()
	p := &platformState{}
	if ok {
		p.prevIdle = idle
		p.prevTotal = kernel + user
	}
	return p
}

func (p *platformState) sample(diskPath string) Snapshot {
	var snap Snapshot

	if idle, kernel, user, ok := readSystemTimes(); ok {
		total := kernel + user
		deltaIdle := float64(idle - p.prevIdle)
		deltaTotal := float64(total - p.prevTotal)
		if deltaTotal > 0 {
			snap.CPUPercent = (1 - deltaIdle/deltaTotal) * 100
		}
		p.prevIdle, p.prevTotal = idle, total
		snap.CPUAvailable = true
	}

	if used, total, ok := readMemInfo(); ok {
		snap.MemUsedBytes, snap.MemTotalBytes = used, total
		snap.MemAvailable = true
	}

	if used, total, ok := readDiskUsage(diskPath); ok {
		snap.DiskUsedBytes, snap.DiskTotalBytes = used, total
		snap.DiskAvailable = true
	}

	return snap
}

// memoryStatusEx mirrors Win32's MEMORYSTATUSEX struct exactly (field order
// and sizes matter -- this is passed by pointer straight into the kernel32
// call). Stable, documented Win32 ABI, unchanged since Windows 2000.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func readMemInfo() (usedBytes, totalBytes uint64, ok bool) {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return 0, 0, false
	}
	return m.TotalPhys - m.AvailPhys, m.TotalPhys, true
}

// readSystemTimes wraps GetSystemTimes. Per Win32 docs, lpKernelTime
// already includes idle time, so total-busy-time is kernel+user and idle
// alone is subtracted the same way Linux's idle+iowait is -- same
// delta-between-two-samples percentage calculation as the Linux
// implementation, just a different counter source.
func readSystemTimes() (idle, kernel, user uint64, ok bool) {
	var idleFT, kernelFT, userFT syscall.Filetime
	r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleFT)),
		uintptr(unsafe.Pointer(&kernelFT)),
		uintptr(unsafe.Pointer(&userFT)),
	)
	if r == 0 {
		return 0, 0, 0, false
	}
	return filetimeToUint64(idleFT), filetimeToUint64(kernelFT), filetimeToUint64(userFT), true
}

func filetimeToUint64(ft syscall.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

func readDiskUsage(path string) (usedBytes, totalBytes uint64, ok bool) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	var freeAvailable, total, totalFree uint64
	r, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeAvailable)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, 0, false
	}
	return total - totalFree, total, true
}

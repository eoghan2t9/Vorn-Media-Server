package sysstats

import (
	"strings"
	"testing"
)

func TestParseCPUStat(t *testing.T) {
	// user nice system idle iowait irq softirq steal guest guest_nice
	const fixture = `cpu  100 10 50 800 5 2 1 0 0 0
cpu0 50 5 25 400 2 1 0 0 0 0
intr 12345
`
	idle, total, err := parseCPUStat(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("parseCPUStat: %v", err)
	}
	wantIdle := uint64(800 + 5)
	wantTotal := uint64(100 + 10 + 50 + 800 + 5 + 2 + 1 + 0 + 0 + 0)
	if idle != wantIdle || total != wantTotal {
		t.Errorf("got idle=%d total=%d, want idle=%d total=%d", idle, total, wantIdle, wantTotal)
	}
}

func TestParseCPUStatMalformed(t *testing.T) {
	cases := []string{"", "not cpu data\n", "cpu\n"}
	for _, c := range cases {
		if _, _, err := parseCPUStat(strings.NewReader(c)); err == nil {
			t.Errorf("parseCPUStat(%q): expected an error, got nil", c)
		}
	}
}

func TestParseMemInfo(t *testing.T) {
	const fixture = `MemTotal:       16384000 kB
MemFree:         2048000 kB
MemAvailable:    8192000 kB
Buffers:          512000 kB
`
	used, total, err := parseMemInfo(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("parseMemInfo: %v", err)
	}
	wantTotal := uint64(16384000 * 1024)
	wantUsed := uint64((16384000 - 8192000) * 1024)
	if total != wantTotal || used != wantUsed {
		t.Errorf("got used=%d total=%d, want used=%d total=%d", used, total, wantUsed, wantTotal)
	}
}

func TestParseMemInfoMissingTotal(t *testing.T) {
	if _, _, err := parseMemInfo(strings.NewReader("MemFree: 100 kB\n")); err == nil {
		t.Error("expected an error when MemTotal is missing")
	}
}

func TestSamplerLatestBeforeFirstSample(t *testing.T) {
	s := &Sampler{}
	snap := s.Latest()
	if snap.Available {
		t.Error("expected the zero-value Snapshot before any background sample has run")
	}
}

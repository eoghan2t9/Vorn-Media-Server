// Package transcode wraps ffmpeg/ffprobe (via os/exec, never CGO) for
// hardware-capability probing and on-the-fly HLS transcoding.
package transcode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

// Backend describes one candidate hardware (or software) encoder path.
type Backend struct {
	Name       string // e.g. "vaapi:/dev/dri/renderD128", "nvenc", "qsv", "videotoolbox", "software"
	Encoder    string // ffmpeg -c:v value, e.g. "h264_vaapi"
	DeviceArgs []string
	FilterArgs []string
}

const probeTimeout = 8 * time.Second

// DetectBackends runs a real, short test encode against every hardware
// backend plausible on the current OS, plus the software fallback, and
// returns only the ones that actually worked. Static enumeration (parsing
// `ffmpeg -encoders`) is not enough: a compiled-in encoder can still fail at
// runtime because of driver problems, missing render-node permissions
// inside a container, or -- as observed on this exact box, whose "GPU" is
// QEMU/virtio -- a paravirtualized device that libva simply can't drive.
// Only a real probe encode catches that.
func DetectBackends(ctx context.Context) []Backend {
	var candidates []Backend
	switch runtime.GOOS {
	case "linux":
		candidates = append(candidates, vaapiCandidates()...)
		candidates = append(candidates, Backend{Name: "nvenc", Encoder: "h264_nvenc"})
		candidates = append(candidates, Backend{Name: "qsv", Encoder: "h264_qsv", DeviceArgs: []string{"-init_hw_device", "qsv=hw"}})
	case "windows":
		candidates = append(candidates,
			Backend{Name: "nvenc", Encoder: "h264_nvenc"},
			Backend{Name: "qsv", Encoder: "h264_qsv", DeviceArgs: []string{"-init_hw_device", "qsv=hw"}},
			Backend{Name: "amf", Encoder: "h264_amf"},
		)
	case "darwin":
		candidates = append(candidates, Backend{Name: "videotoolbox", Encoder: "h264_videotoolbox"})
	}
	candidates = append(candidates, Backend{Name: "software", Encoder: "libx264"})

	var working []Backend
	for _, b := range candidates {
		if probeBackend(ctx, b) {
			working = append(working, b)
		}
	}
	return working
}

// vaapiCandidates returns one Backend per /dev/dri/render* node found, since
// a multi-GPU host (e.g. Intel iGPU + AMD dGPU) exposes VAAPI through
// separate render nodes and either might be the one that actually works.
func vaapiCandidates() []Backend {
	entries, err := os.ReadDir("/dev/dri")
	if err != nil {
		return nil
	}
	var out []Backend
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if len(name) < 6 || name[:6] != "render" {
			continue
		}
		dev := filepath.Join("/dev/dri", name)
		out = append(out, Backend{
			Name:       "vaapi:" + dev,
			Encoder:    "h264_vaapi",
			DeviceArgs: []string{"-vaapi_device", dev},
			FilterArgs: []string{"-vf", "format=nv12,hwupload"},
		})
	}
	return out
}

// probeBackend attempts a one-second, single-frame encode into /dev/null
// (via ffmpeg's null muxer) and reports whether ffmpeg exited 0.
func probeBackend(ctx context.Context, b Backend) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	args := []string{"-hide_banner", "-y"}
	args = append(args, b.DeviceArgs...)
	args = append(args, "-f", "lavfi", "-i", "testsrc=size=320x240:rate=1:duration=1")
	args = append(args, b.FilterArgs...)
	args = append(args, "-c:v", b.Encoder, "-frames:v", "1", "-f", "null", "-")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd.Run() == nil
}

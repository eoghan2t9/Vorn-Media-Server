package nzb

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const torBoxCacheTimeout = 20 * time.Minute

// runTorBox fulfils rec by handing the raw .nzb off to a TorBox account
// instead of dialing a real Usenet server: TorBox downloads, yEnc decodes,
// and par2-repairs it against its own Usenet backend, and Vorn then just
// fetches the finished files over HTTPS into the usual local save path so
// promotion (nzb.PromoteCompleted) works exactly as it does for the NNTP
// path. The remote caching/repair phase is reported under the existing
// "repairing" status (accurate -- that's genuinely what's happening, just
// off-box) before switching to "downloading" for the local file fetch.
func (svc *Service) runTorBox(rec *store.NZBDownload, data []byte, server *store.UsenetServer) {
	client := debrid.NewTorBoxClient()
	ctx, cancel := context.WithTimeout(context.Background(), torBoxCacheTimeout)
	defer cancel()

	if err := svc.store.UpdateNZBProgress(rec.ID, 10000, 0, "repairing"); err != nil {
		log.Printf("nzb: setting status for %s: %v", rec.ID, err)
	}

	usenetID, err := client.CreateUsenetDownload(ctx, server.APIKey, data, rec.Name)
	if err != nil {
		svc.finish(rec, fmt.Errorf("torbox: %w", err))
		return
	}

	files, err := client.WaitForUsenetCache(ctx, server.APIKey, usenetID, func(frac float64) {
		if err := svc.store.UpdateNZBProgress(rec.ID, 10000, int64(frac*10000), "repairing"); err != nil {
			log.Printf("nzb: updating progress for %s: %v", rec.ID, err)
		}
	})
	if err != nil {
		svc.finish(rec, err)
		return
	}

	outDir := filepath.Join(rec.SavePath, rec.Name)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		svc.finish(rec, err)
		return
	}

	var total int64
	for _, f := range files {
		total += f.Size
	}
	if err := svc.store.UpdateNZBProgress(rec.ID, total, 0, "downloading"); err != nil {
		log.Printf("nzb: setting total bytes for %s: %v", rec.ID, err)
	}

	progress := &progressReporter{store: svc.store, recID: rec.ID, total: total}
	var done atomic.Int64

	for _, f := range files {
		name := f.Name
		if name == "" {
			name = f.ShortName
		}
		link, err := client.RequestUsenetDownloadLink(ctx, server.APIKey, usenetID, f.ID)
		if err != nil {
			svc.finish(rec, fmt.Errorf("torbox: requesting download link for %s: %w", name, err))
			return
		}
		if err := downloadToFile(ctx, link, filepath.Join(outDir, name), &done, progress); err != nil {
			svc.finish(rec, fmt.Errorf("torbox: downloading %s: %w", name, err))
			return
		}
	}
	progress.forceReport(done.Load(), "downloading")

	svc.finish(rec, nil)
	if svc.onComplete != nil {
		fresh, err := svc.store.GetNZBDownload(rec.ID)
		if err != nil {
			log.Printf("nzb: reloading %s for completion callback: %v", rec.ID, err)
			return
		}
		svc.onComplete(fresh)
	}
}

func downloadToFile(ctx context.Context, url, dest string, done *atomic.Int64, progress *progressReporter) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	counter := &progressCounter{done: done, progress: progress}
	_, err = io.Copy(out, io.TeeReader(resp.Body, counter))
	return err
}

// progressCounter is an io.Writer sink for io.TeeReader that turns bytes
// read from the response body into progress updates, without needing its
// own buffering.
type progressCounter struct {
	done     *atomic.Int64
	progress *progressReporter
}

func (w *progressCounter) Write(p []byte) (int, error) {
	total := w.done.Add(int64(len(p)))
	w.progress.report(total)
	return len(p), nil
}

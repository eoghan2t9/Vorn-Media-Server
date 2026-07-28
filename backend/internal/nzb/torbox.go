package nzb

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"golang.org/x/sync/errgroup"
)

const torBoxCacheTimeout = 20 * time.Minute

// runTorBox fulfils rec by handing the raw .nzb off to a TorBox account:
// TorBox downloads, yEnc decodes, and par2-repairs it against its own
// Usenet backend. Vorn never fetches the resulting bytes itself --
// TorBox's RequestUsenetDownloadLink returns a direct HTTP stream URL per
// file, the same kind of provider-hosted CDN link debrid resolves to, so
// each one is just recorded as an NZBFile row (mirroring debrid_files) and
// promotion points the media item straight at it. No local disk space is
// ever used. The remote caching/repair phase is reported under the
// existing "repairing" status (accurate -- that's genuinely what's
// happening, just off-box).
func (svc *Service) runTorBox(parentCtx context.Context, rec *store.NZBDownload, data []byte, server *store.UsenetServer) {
	client := svc.torboxClient
	ctx, cancel := context.WithTimeout(parentCtx, torBoxCacheTimeout)
	defer cancel()

	if err := svc.store.UpdateNZBProgress(rec.ID, 10000, 0, "repairing"); err != nil {
		log.Printf("nzb: setting status for %s: %v", rec.ID, err)
	}

	usenetID, err := client.CreateUsenetDownload(ctx, server.APIKey, data, rec.Name)
	if err != nil {
		svc.finish(rec, fmt.Errorf("torbox: %w", err))
		return
	}
	if err := svc.store.SetNZBDownloadProviderRef(rec.ID, strconv.Itoa(usenetID)); err != nil {
		log.Printf("nzb: recording provider ref for %s: %v", rec.ID, err)
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

	var total int64
	for _, f := range files {
		total += f.Size
	}

	g, gCtx := errgroup.WithContext(ctx)
	for _, f := range files {
		f := f
		g.Go(func() error {
			name := f.Name
			if name == "" {
				name = f.ShortName
			}
			link, err := client.RequestUsenetDownloadLink(gCtx, server.APIKey, usenetID, f.ID)
			if err != nil {
				return fmt.Errorf("torbox: requesting download link for %s: %w", name, err)
			}
			if _, err := svc.store.AddNZBFile(rec.ID, name, f.Size, link); err != nil {
				return fmt.Errorf("torbox: recording stream url for %s: %w", name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		svc.finish(rec, err)
		return
	}
	if err := svc.store.UpdateNZBProgress(rec.ID, total, total, "repairing"); err != nil {
		log.Printf("nzb: updating progress for %s: %v", rec.ID, err)
	}

	svc.finish(rec, nil)
	if svc.onComplete == nil {
		return
	}
	fresh, err := svc.store.GetNZBDownload(rec.ID)
	if err != nil {
		log.Printf("nzb: reloading %s for completion callback: %v", rec.ID, err)
		return
	}
	if used := svc.onComplete(fresh); !used {
		svc.deleteFromTorBox(fresh.ID, fresh.ProviderRef)
	}
}

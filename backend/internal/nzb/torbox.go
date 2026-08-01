package nzb

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/webdav"
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

	// After all CDN stream URLs are stored, match each file against the
	// WebDAV server by size to find its stable (non-expiring) WebDAV URL.
	// TorBox's WebDAV serves NZB-downloaded files under random hash
	// filenames, so the only shared key between the API response and the
	// WebDAV listing is the file size. Doing this before finish/onComplete
	// means promotion sees the WebDAV URL and can use it as the primary
	// media_item Path.
	svc.matchWebDAVURLs(ctx, rec)

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

const matchWebDAVTimeout = 30 * time.Second

// matchWebDAVURLs walks each WebDAV folder attached to rec's library and
// matches nzb_files by size against discovered WebDAV files, storing the
// stable WebDAV URL on each matched nzb_file. TorBox's WebDAV serves
// NZB-cached files under random hash filenames, so the only shared key
// between the API response (real name + size) and the WebDAV listing
// (hash name + size) is the file size. Best-effort — any failure
// (no WebDAV folder, network error, no match) is logged and ignored;
// promotion falls back to the CDN stream URL when no WebDAV URL is stored.
func (svc *Service) matchWebDAVURLs(ctx context.Context, rec *store.NZBDownload) {
	if rec.LibraryID == nil {
		return
	}
	folders, err := svc.store.ListWebDAVFoldersByLibrary(*rec.LibraryID)
	if err != nil || len(folders) == 0 {
		return
	}

	files, err := svc.store.ListNZBFiles(rec.ID)
	if err != nil {
		return
	}

	// Build a size→file map for matching — only video files are
	// relevant (non-video files are skipped by promotion anyway).
	bySize := make(map[int64]*store.NZBFile, len(files))
	for _, f := range files {
		if scanner.IsVideoFile(f.Name) && f.WebDAVURL == "" {
			bySize[f.SizeBytes] = f
		}
	}
	if len(bySize) == 0 {
		return
	}

	for _, folder := range folders {
		if !folder.Enabled {
			continue
		}
		matchCtx, cancel := context.WithTimeout(ctx, matchWebDAVTimeout)
		// Refresh the WebDAV index first so newly cached NZB files appear
		// without waiting for the server's own 15-minute refresh cycle —
		// same pattern the scanner's WebDAV walker uses.
		if err := webdav.Refresh(matchCtx, folder.URL, folder.APIKey); err != nil {
			log.Printf("nzb: refreshing webdav index for %s: %v", folder.URL, err)
		}
		if err := webdav.Walk(matchCtx, folder.URL, folder.APIKey, func(f webdav.DiscoveredFile) {
			if nf, ok := bySize[f.SizeBytes]; ok {
				if err := svc.store.SetNZBFileWebDAVURL(nf.ID, f.Path); err != nil {
					log.Printf("nzb: storing webdav url for %s: %v", nf.ID, err)
					return
				}
				delete(bySize, f.SizeBytes) // only match once
			}
		}); err != nil {
			log.Printf("nzb: matching webdav urls for %s against %s: %v", rec.ID, folder.URL, err)
		}
		cancel()

		if len(bySize) == 0 {
			break // all matched
		}
	}
}

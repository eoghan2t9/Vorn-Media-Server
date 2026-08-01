package nzb

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
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

	usenetID, webdavHash, err := client.CreateUsenetDownload(ctx, server.APIKey, data, rec.Name)
	if err != nil {
		svc.finish(rec, fmt.Errorf("torbox: %w", err))
		return
	}
	// Store both the usenet ID (for delete/control operations) and the
	// WebDAV hash (for walking the exact directory where cached files
	// will appear) as provider_ref — the format "<usenetID>:<hash>"
	// is parsed by matchWebDAVURLs and deleteFromTorBox.
	if err := svc.store.SetNZBDownloadProviderRef(rec.ID, strconv.Itoa(usenetID)+":"+webdavHash); err != nil {
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

// matchWebDAVURLs walks the WebDAV directory for rec's cached files
// (constructed from the hash returned by CreateUsenetDownload) and matches
// nzb_files by size+extension against discovered WebDAV files, storing
// the stable WebDAV URL on each matched nzb_file. Previously this walked
// the root WebDAV URL and relied on the server listing every subdirectory,
// but TorBox's root PROPFIND returns 404 — the hash from the creation
// response gives us the exact directory to walk directly, which is both
// faster and actually works. Best-effort: any failure is logged and
// ignored; promotion falls back to the CDN stream URL.
func (svc *Service) matchWebDAVURLs(ctx context.Context, rec *store.NZBDownload) {
	// Extract the WebDAV hash from provider_ref, which now stores
	// "<usenetID>:<hash>" (see runTorBox).
	webdavHash := webdavHashFromProviderRef(rec.ProviderRef)
	if webdavHash == "" {
		return
	}

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

	type matchKey struct {
		size int64
		ext  string
	}
	byKey := make(map[matchKey]*store.NZBFile, len(files))
	for _, f := range files {
		if scanner.IsVideoFile(f.Name) && f.WebDAVURL == "" {
			byKey[matchKey{f.SizeBytes, filepath.Ext(f.Name)}] = f
		}
	}
	if len(byKey) == 0 {
		return
	}

	// Walk the exact hash directory on the WebDAV server — no need to
	// walk the root or enumerate subdirectories since we know the path.
	for _, folder := range folders {
		if !folder.Enabled {
			continue
		}
		dirURL := folder.URL + "/" + webdavHash + "/"
		matchCtx, cancel := context.WithTimeout(ctx, matchWebDAVTimeout)
		if err := webdav.Refresh(matchCtx, dirURL, folder.APIKey); err != nil {
			log.Printf("nzb: refreshing webdav index for %s: %v", dirURL, err)
		}
		if err := webdav.Walk(matchCtx, dirURL, folder.APIKey, func(f webdav.DiscoveredFile) {
			key := matchKey{f.SizeBytes, filepath.Ext(f.Path)}
			if nf, ok := byKey[key]; ok {
				if err := svc.store.SetNZBFileWebDAVURL(nf.ID, f.Path); err != nil {
					log.Printf("nzb: storing webdav url for %s: %v", nf.ID, err)
					return
				}
				delete(byKey, key)
			}
		}); err != nil {
			log.Printf("nzb: matching webdav urls for %s against %s: %v", rec.ID, dirURL, err)
		}
		cancel()

		if len(byKey) == 0 {
			break
		}
	}
}

// webdavHashFromProviderRef extracts the WebDAV hash from a provider_ref
// that was stored in the format "<usenetID>:<hash>". For older records
// that only have the usenet ID (no colon), returns "".
func webdavHashFromProviderRef(ref string) string {
	if idx := strings.LastIndex(ref, ":"); idx >= 0 && idx < len(ref)-1 {
		return ref[idx+1:]
	}
	return ""
}

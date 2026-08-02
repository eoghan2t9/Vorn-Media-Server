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
)

// runTorBox creates a usenet download on TorBox, stores the provider_ref,
// sets the status to "repairing", and returns immediately. The continuous
// background sync (syncFromTorBox) detects when the download finishes on
// TorBox's side and handles the rest: recording CDN stream URLs per file,
// matching WebDAV URLs, and promoting the result. This mirrors rdt-client's
// ProviderUpdater: no per-download goroutine ever blocks polling — one
// periodic sweep catches every download in a single API call.
func (svc *Service) runTorBox(parentCtx context.Context, rec *store.NZBDownload, data []byte, server *store.UsenetServer) {
	client := svc.torboxClient
	ctx, cancel := context.WithTimeout(parentCtx, 2*time.Minute)
	defer cancel()

	if err := svc.store.UpdateNZBProgress(rec.ID, 10000, 0, "repairing"); err != nil {
		log.Printf("nzb: setting status for %s: %v", rec.ID, err)
	}

	usenetID, webdavHash, err := client.CreateUsenetDownload(ctx, server.APIKey, data, rec.Name)
	if err != nil {
		// Retry once on transient errors (rate limiting, temporary
		// server unavailability) — a single 429 or 503 from TorBox
		// shouldn't kill an otherwise viable candidate, especially
		// when the parallel torrent tier is also racing and a failed
		// NZB means one less chance of any stream at all.
		if isTransientError(err) {
			log.Printf("nzb: transient error creating usenet download for %s, retrying: %v", rec.ID, err)
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				svc.finish(rec, fmt.Errorf("torbox: %w", err))
				return
			}
			usenetID, webdavHash, err = client.CreateUsenetDownload(ctx, server.APIKey, data, rec.Name)
		}
		if err != nil {
			svc.finish(rec, fmt.Errorf("torbox: %w", err))
			return
		}
	}
	// Store both the usenet ID (for delete/control operations) and the
	// WebDAV hash (for walking the exact directory where cached files
	// will appear) as provider_ref — the format "<usenetID>:<hash>"
	// is parsed by matchWebDAVURLs and deleteFromTorBox.
	if err := svc.store.SetNZBDownloadProviderRef(rec.ID, strconv.Itoa(usenetID)+":"+webdavHash); err != nil {
		log.Printf("nzb: recording provider ref for %s: %v", rec.ID, err)
	}
	// The rest — waiting for TorBox to finish caching, recording file
	// entries, matching WebDAV URLs, and promoting — is handled by the
	// continuous background poller (syncFromTorBox) which catches every
	// download on each sweep, no per-download goroutine needed.
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

// isTransientError reports whether err is likely to be a transient
// server-side issue (rate limiting, temporary unavailability) that a
// single retry after a short delay has a good chance of clearing,
// rather than a permanent failure (bad API key, invalid request, etc).
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// TorBox returns HTTP 429 for rate limiting and 502/503/504 for
	// temporary server issues — all worth one retry.
	return strings.Contains(s, "429") ||
		strings.Contains(s, "502") ||
		strings.Contains(s, "503") ||
		strings.Contains(s, "504") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "temporarily unavailable")
}

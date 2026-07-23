package nzb

import (
	"log"
	"path/filepath"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// PromoteCompleted turns a finished NZB download's video files into
// browsable media_items via scanner.PromoteDirectory, the same ingestion
// tail end the filesystem scanner and torrent watcher use. A download with
// no destination library, or one already promoted, is skipped.
func PromoteCompleted(st *store.Store, n *store.NZBDownload) {
	if n.LibraryID == nil {
		log.Printf("nzb: %s (%s) completed with no destination library; skipping auto-add", n.ID, n.Name)
		return
	}
	if n.Promoted {
		return
	}

	root := filepath.Join(n.SavePath, n.Name)
	if err := scanner.PromoteDirectory(st, *n.LibraryID, root); err != nil {
		log.Printf("nzb: promoting files under %s: %v", root, err)
		return
	}
	if err := st.MarkNZBPromoted(n.ID); err != nil {
		log.Printf("nzb: marking %s promoted: %v", n.ID, err)
	}
}

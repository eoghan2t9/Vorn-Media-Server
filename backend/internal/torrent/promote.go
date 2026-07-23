package torrent

import (
	"log"
	"path/filepath"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// PromoteCompleted turns a finished torrent's video files into browsable
// media_items via scanner.PromoteDirectory, the same ingestion tail end the
// filesystem scanner uses. A torrent with no destination library, or one
// already promoted, is skipped.
func PromoteCompleted(st *store.Store, t *store.Torrent) {
	if t.LibraryID == nil {
		log.Printf("torrent: %s (%s) completed with no destination library; skipping auto-add", t.ID, t.Name)
		return
	}
	if t.Promoted {
		return
	}

	root := filepath.Join(t.SavePath, t.Name)
	if err := scanner.PromoteDirectory(st, *t.LibraryID, root); err != nil {
		log.Printf("torrent: promoting files under %s: %v", root, err)
		return
	}
	if err := st.MarkTorrentPromoted(t.ID); err != nil {
		log.Printf("torrent: marking %s promoted: %v", t.ID, err)
	}
}

// Package torrent integrates BitTorrent acquisition into Vorn behind the
// same ingestion path the filesystem scanner uses: downloads land on disk
// under a save directory and, once complete, get promoted into media_items
// exactly like manually-placed files.
package torrent

import (
	lt "github.com/anacrolix/torrent"
)

func newClient(downloadDir string, peerPort int) (*lt.Client, error) {
	cfg := lt.NewDefaultClientConfig()
	cfg.DataDir = downloadDir
	if peerPort > 0 {
		cfg.ListenPort = peerPort
	}
	return lt.NewClient(cfg)
}

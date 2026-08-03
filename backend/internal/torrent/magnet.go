package torrent

import (
	"bytes"
	"fmt"

	"github.com/anacrolix/torrent/metainfo"
)

// MagnetFromTorrentBytes extracts a magnet URI out of raw .torrent file
// bytes -- pure bencode parsing (the metainfo subpackage has no
// BitTorrent-networking code in it at all), so this touches no peers/swarm.
// Used so an uploaded .torrent file can be resolved through a debrid
// provider exactly like a pasted magnet link, without Vorn ever downloading
// the torrent itself.
func MagnetFromTorrentBytes(data []byte) (string, error) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("torrent: parsing torrent file: %w", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return "", fmt.Errorf("torrent: reading torrent info: %w", err)
	}
	hash := mi.HashInfoBytes()
	return mi.Magnet(&hash, &info).String(), nil
}

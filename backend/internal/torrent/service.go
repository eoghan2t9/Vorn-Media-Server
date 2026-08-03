// Package torrent finds torrent-protocol releases via Torznab-compatible
// indexers (Prowlarr, Jackett, TorBox's own search API). Vorn never
// downloads torrent data itself -- a found release's magnet/hash is always
// resolved through a debrid provider (see internal/debrid), which fetches
// from the swarm on its own infrastructure and hands back a direct stream
// URL. See MagnetFromTorrentBytes for the one local exception: extracting
// the magnet URI out of an uploaded .torrent file needs no swarm activity
// at all, just parsing the file's own bencoded metadata.
package torrent

import (
	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// Service searches configured indexers for torrent releases. It holds no
// download state -- resolving a found release into something playable is
// entirely debrid.Service's job (see debrid.Service.AddLink).
type Service struct {
	store *store.Store
	// torboxLimiter is the one shared rate limiter for every TorBox
	// interaction this process makes, across all three services that talk
	// to it (this one's TorBox indexer search, debrid.Service's resolve
	// client, nzb.Service's usenet caching) -- see debrid.Service.TorBoxLimiter.
	torboxLimiter *debrid.Limiter
}

// NewService takes torboxLimiter (see debrid.Service.TorBoxLimiter) rather
// than constructing its own, so this Service's TorBox indexer search shares
// the exact same rate budget as debrid.Service's TorBox debrid-resolve
// client and nzb.Service's TorBox usenet caching.
func NewService(st *store.Store, torboxLimiter *debrid.Limiter) *Service {
	return &Service{store: st, torboxLimiter: torboxLimiter}
}

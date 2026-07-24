// Package debrid resolves magnet links / info-hashes against Real-Debrid and
// TorBox cloud-caching accounts, turning them into direct HTTP stream URLs
// that require no local download.
package debrid

import "context"

// ResolvedFile is one playable file produced by adding a magnet link (or
// info-hash) to a debrid provider's cloud torrent client.
type ResolvedFile struct {
	Name      string
	SizeBytes int64
	StreamURL string
}

// AccountInfo is basic account status from a debrid provider, fetched with
// a single lightweight call so an admin can verify an API key is valid
// without waiting on a real magnet resolve.
type AccountInfo struct {
	Username string
	Premium  bool
	Detail   string // human-readable extra context (plan/expiry), provider-specific
}

// Provider adds a magnet link/info-hash to a debrid service's cloud storage
// and, once the provider has cached it, returns direct unrestricted
// stream/download URLs for its files.
type Provider interface {
	Name() string
	Resolve(ctx context.Context, apiKey, magnetOrHash string) ([]ResolvedFile, error)
	AccountInfo(ctx context.Context, apiKey string) (*AccountInfo, error)
}

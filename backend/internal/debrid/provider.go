// Package debrid resolves magnet links / info-hashes against Real-Debrid and
// TorBox cloud-caching accounts, turning them into direct HTTP stream URLs
// that require no local download.
package debrid

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// ResolvedFile is one playable file produced by adding a magnet link (or
// info-hash) to a debrid provider's cloud torrent client.
type ResolvedFile struct {
	Name      string
	SizeBytes int64
	StreamURL string
}

// ResolveResult is what a successful Resolve produces: the files themselves,
// plus the provider's own id for this resolve (torrent id, magnet id,
// transfer id...) so it can be deleted later via Delete once Vorn no longer
// needs it -- without this, every resolve permanently consumes an account
// slot with no way to reclaim it.
type ResolveResult struct {
	ProviderRef string
	Files       []ResolvedFile
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
	Resolve(ctx context.Context, apiKey, magnetOrHash string) (*ResolveResult, error)
	// Delete removes providerRef (as returned in a prior ResolveResult) from
	// the account, reclaiming whatever storage/active-download quota it held.
	// A no-op (nil error) is expected if providerRef is empty or already gone.
	Delete(ctx context.Context, apiKey, providerRef string) error
	AccountInfo(ctx context.Context, apiKey string) (*AccountInfo, error)
}

// CacheChecker is an optional capability some Provider implementations
// support: checking whether a batch of info-hashes are already cached on
// the provider's side before committing to a full add+poll resolve for any
// of them. Not part of Provider itself since only some providers currently
// expose a real endpoint for this (Real-Debrid, TorBox) -- AllDebrid and
// Debrid-Link have both removed their equivalents entirely, so callers
// type-assert for this rather than every Provider being forced to
// implement it.
type CacheChecker interface {
	CheckCached(ctx context.Context, apiKey string, hashes []string) (map[string]bool, error)
}

// FailureKind categorizes a debrid provider error so a poll loop can react
// appropriately instead of treating every failure the same way: stop
// immediately on a permanent failure, back off and retry on a rate limit,
// keep polling (or fail after the normal timeout) on anything unrecognized.
type FailureKind int

const (
	FailureUnknown FailureKind = iota
	// FailurePermanent means the content itself will never resolve (dead
	// torrent, missing articles, banned item) -- stop polling now instead of
	// waiting out the full timeout.
	FailurePermanent
	// FailureAuth means the API key is bad/expired -- stop immediately,
	// don't burn quota retrying with credentials that will never work.
	FailureAuth
	// FailureQuotaExceeded means an account-level limit was hit (storage,
	// active downloads, fair-use) -- stop immediately with a clear message.
	FailureQuotaExceeded
	// FailureRateLimited means a 429 -- back off RetryAfter (if known) and
	// retry within whatever's left of the caller's own timeout, rather than
	// aborting the whole resolve over a transient rate limit.
	FailureRateLimited
)

// ClassifiedError wraps a provider error with enough information for a poll
// loop to decide whether to stop immediately, back off and retry, or treat
// it like any other transient failure.
type ClassifiedError struct {
	Kind       FailureKind
	RetryAfter time.Duration // only meaningful when Kind == FailureRateLimited
	Err        error
}

func (e *ClassifiedError) Error() string { return e.Err.Error() }
func (e *ClassifiedError) Unwrap() error { return e.Err }

// retryAfter parses a 429 response's Retry-After header, which providers
// send as either a number of seconds or an HTTP-date -- 0 if absent or
// unparseable, letting callers fall back to their own default backoff.
func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// capRetry bounds a rate-limit backoff to whatever's left before deadline,
// so a poll loop never oversleeps past its own timeout on a long
// Retry-After -- and never returns a negative/zero duration that would spin.
func capRetry(d time.Duration, deadline time.Time) time.Duration {
	if remaining := time.Until(deadline); d > remaining {
		d = remaining
	}
	if d < 0 {
		d = 0
	}
	return d
}

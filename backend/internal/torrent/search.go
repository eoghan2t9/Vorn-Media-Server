package torrent

import (
	"context"
	"log"
	"sync"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// Search queries every enabled Torznab indexer concurrently and merges the
// results. One indexer failing (timeout, bad config, ...) is logged and
// skipped rather than failing the whole search. TorBox-provider indexers
// are skipped here -- they don't speak Torznab's free-text q= search at
// all, see SearchByIMDb.
func (svc *Service) Search(ctx context.Context, query string) ([]SearchResult, error) {
	indexers, err := svc.store.ListTorrentIndexers()
	if err != nil {
		return nil, err
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []SearchResult
	)
	for _, idx := range indexers {
		if !idx.Enabled || idx.Provider == "torbox" {
			continue
		}
		wg.Add(1)
		go func(idx *store.TorrentIndexer) {
			defer wg.Done()
			res, err := SearchIndexer(ctx, idx.Name, idx.BaseURL, idx.APIKey, query)
			if err != nil {
				log.Printf("torrent: searching indexer %s: %v", idx.Name, err)
				return
			}
			mu.Lock()
			results = append(results, res...)
			mu.Unlock()
		}(idx)
	}
	wg.Wait()
	return results, nil
}

// SearchByIMDb is Search's TorBox-provider counterpart: TorBox's own
// torrent-search API takes no free-text query at all, only an IMDb ID (see
// searchTorBoxIndexer) -- season==0 means "movie", season>0 means
// "episode" (episode defaults to 1 if not given, since there's no "whole
// season" query mode). Returns (nil, nil) immediately if imdbID is empty
// rather than querying every TorBox indexer for nothing, since a caller
// with no IMDb ID on hand for this item can't usefully call this at all.
func (svc *Service) SearchByIMDb(ctx context.Context, imdbID string, season, episode int) ([]SearchResult, error) {
	if imdbID == "" {
		return nil, nil
	}
	indexers, err := svc.store.ListTorrentIndexers()
	if err != nil {
		return nil, err
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []SearchResult
	)
	for _, idx := range indexers {
		if !idx.Enabled || idx.Provider != "torbox" {
			continue
		}
		wg.Add(1)
		go func(idx *store.TorrentIndexer) {
			defer wg.Done()
			if err := svc.torboxLimiter.Wait(ctx); err != nil {
				return
			}
			res, err := searchTorBoxIndexer(ctx, idx.Name, idx.APIKey, imdbID, season, episode)
			if err != nil {
				log.Printf("torrent: searching TorBox indexer %s: %v", idx.Name, err)
				return
			}
			mu.Lock()
			results = append(results, res...)
			mu.Unlock()
		}(idx)
	}
	wg.Wait()
	return results, nil
}

func (svc *Service) AddIndexer(name, baseURL, apiKey, provider string) (*store.TorrentIndexer, error) {
	return svc.store.CreateTorrentIndexer(name, baseURL, apiKey, provider)
}

func (svc *Service) ListIndexers() ([]*store.TorrentIndexer, error) {
	return svc.store.ListTorrentIndexers()
}

func (svc *Service) RemoveIndexer(id string) error {
	return svc.store.DeleteTorrentIndexer(id)
}

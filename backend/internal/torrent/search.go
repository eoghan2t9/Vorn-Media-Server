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

// SearchByIMDb is Search's id-based counterpart, covering both indexer
// kinds: regular Torznab indexers' t=movie/t=tvsearch (see
// SearchIndexerByIMDb -- the torrent-side sibling of Newznab, same
// protocol family), and TorBox-provider indexers' own search-api (which
// takes no free-text query at all, only an id -- see searchTorBoxIndexer;
// only imdbID applies there today, tvdbID is Torznab-only for now).
// Returns (nil, nil) immediately if both ids are empty rather than
// querying every indexer for nothing.
func (svc *Service) SearchByIMDb(ctx context.Context, imdbID, tvdbID string, season, episode int) ([]SearchResult, error) {
	if imdbID == "" && tvdbID == "" {
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
		if !idx.Enabled {
			continue
		}
		wg.Add(1)
		go func(idx *store.TorrentIndexer) {
			defer wg.Done()
			var res []SearchResult
			var err error
			if idx.Provider == "torbox" {
				if imdbID == "" {
					return // TorBox's search-api only takes imdbID today
				}
				if waitErr := svc.torboxLimiter.Wait(ctx); waitErr != nil {
					return
				}
				res, err = searchTorBoxIndexer(ctx, idx.Name, idx.APIKey, imdbID, season, episode)
			} else {
				res, err = SearchIndexerByIMDb(ctx, idx.Name, idx.BaseURL, idx.APIKey, imdbID, tvdbID, season, episode)
			}
			if err != nil {
				log.Printf("torrent: searching indexer %s by imdb/tvdb id: %v", idx.Name, err)
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

func (svc *Service) UpdateIndexer(id string, in store.UpdateTorrentIndexerInput) (*store.TorrentIndexer, error) {
	return svc.store.UpdateTorrentIndexer(id, in)
}

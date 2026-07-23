package torrent

import (
	"context"
	"log"
	"sync"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// Search queries every enabled indexer concurrently and merges the results.
// One indexer failing (timeout, bad config, ...) is logged and skipped
// rather than failing the whole search.
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
		if !idx.Enabled {
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

func (svc *Service) AddIndexer(name, baseURL, apiKey string) (*store.TorrentIndexer, error) {
	return svc.store.CreateTorrentIndexer(name, baseURL, apiKey)
}

func (svc *Service) ListIndexers() ([]*store.TorrentIndexer, error) {
	return svc.store.ListTorrentIndexers()
}

func (svc *Service) RemoveIndexer(id string) error {
	return svc.store.DeleteTorrentIndexer(id)
}

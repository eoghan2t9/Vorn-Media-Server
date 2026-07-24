package nzb

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
	indexers, err := svc.store.ListNZBIndexers()
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
		go func(idx *store.NZBIndexer) {
			defer wg.Done()
			res, err := SearchIndexer(ctx, idx.Name, idx.BaseURL, idx.APIKey, query)
			if err != nil {
				log.Printf("nzb: searching indexer %s: %v", idx.Name, err)
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

func (svc *Service) AddIndexer(name, baseURL, apiKey string) (*store.NZBIndexer, error) {
	return svc.store.CreateNZBIndexer(name, baseURL, apiKey)
}

func (svc *Service) ListIndexers() ([]*store.NZBIndexer, error) {
	return svc.store.ListNZBIndexers()
}

func (svc *Service) RemoveIndexer(id string) error {
	return svc.store.DeleteNZBIndexer(id)
}

// AddNZBFromURL fetches a .nzb file from a search result's download URL
// and starts downloading it, the same way AddNZB does for an uploaded file.
func (svc *Service) AddNZBFromURL(ctx context.Context, downloadURL string, libraryID *string) (*store.NZBDownload, error) {
	data, err := FetchNZB(ctx, downloadURL)
	if err != nil {
		return nil, err
	}
	return svc.AddNZB(data, libraryID)
}

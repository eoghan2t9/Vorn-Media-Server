package nzb

import (
	"context"
	"errors"
	"fmt"
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

// SearchByIMDb is Search's id-based counterpart: queries every enabled
// indexer's t=movie/t=tvsearch function instead of the generic t=search
// (see SearchIndexerByIMDb) -- same indexers, no separate config needed.
// Both imdbID and tvdbID are accepted (either may be empty) since which one
// an indexer's tv-search function actually supports varies -- confirmed
// against a live NZBGeek account that its tv-search doesn't accept imdbid
// at all, only tvdbid/rid/tvmazeid, unlike its movie-search which does take
// imdbid. Returns (nil, nil) immediately if both are empty rather than
// querying every indexer for nothing.
func (svc *Service) SearchByIMDb(ctx context.Context, imdbID, tvdbID string, season, episode int) ([]SearchResult, error) {
	if imdbID == "" && tvdbID == "" {
		return nil, nil
	}
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
		// Defense in depth alongside the Enabled filter itself -- see
		// torrent.Service.SearchByIMDb's identical guard for the reasoning.
		if !idx.Enabled || (!idx.SupportsImdbSearch && !idx.SupportsTvdbSearch) {
			continue
		}
		wg.Add(1)
		go func(idx *store.NZBIndexer) {
			defer wg.Done()
			res, err := SearchIndexerByIMDb(ctx, idx.Name, idx.BaseURL, idx.APIKey, imdbID, tvdbID, season, episode)
			if err != nil {
				log.Printf("nzb: searching indexer %s by imdb/tvdb id: %v", idx.Name, err)
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

// AddIndexer registers a new Newznab indexer, rejecting it unless it
// supports the only kind of search Vorn's acquisition system actually uses
// -- id-based (imdbid/tvdbid) search, see resolveImdbSearchParams in
// acquisition/service.go.
func (svc *Service) AddIndexer(ctx context.Context, name, baseURL, apiKey string) (*store.NZBIndexer, error) {
	caps, err := TestIndexer(ctx, baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("nzb: testing indexer: %w", err)
	}
	if !caps.SupportsImdb && !caps.SupportsTvdb {
		return nil, errors.New("nzb: indexer supports neither IMDb nor TVDB id search, which Vorn's acquisition requires")
	}
	return svc.store.CreateNZBIndexer(name, baseURL, apiKey, caps.SupportsImdb, caps.SupportsTvdb)
}

func (svc *Service) ListIndexers() ([]*store.NZBIndexer, error) {
	return svc.store.ListNZBIndexers()
}

func (svc *Service) RemoveIndexer(id string) error {
	return svc.store.DeleteNZBIndexer(id)
}

func (svc *Service) UpdateIndexer(id string, in store.UpdateNZBIndexerInput) (*store.NZBIndexer, error) {
	return svc.store.UpdateNZBIndexer(id, in)
}

// AddNZBFromURL fetches a .nzb file from a search result's download URL
// and starts downloading it, the same way AddNZB does for an uploaded file.
func (svc *Service) AddNZBFromURL(ctx context.Context, downloadURL string, libraryID *string) (*store.NZBDownload, error) {
	data, err := FetchNZB(ctx, downloadURL)
	if err != nil {
		return nil, err
	}
	return svc.AddNZB(context.Background(), data, libraryID)
}

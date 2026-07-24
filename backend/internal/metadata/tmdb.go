// Package metadata matches scanned library items against external metadata
// providers (currently TMDb) for art, overviews, and trailers.
package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	tmdbBaseURL  = "https://api.themoviedb.org/3"
	tmdbImageURL = "https://image.tmdb.org/t/p/w500"
)

// httpDoer is satisfied by *http.Client; tests substitute a fake to avoid
// making real network calls.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// TMDbClient is a thin wrapper over the parts of the TMDb v3 API Vorn needs:
// movie/TV search and trailer lookup. IMDb has no public API, so TMDb (the
// same source Jellyfin/Plex/Emby all use) stands in for it.
type TMDbClient struct {
	apiKey  string
	baseURL string
	client  httpDoer
}

func NewTMDbClient(apiKey string) *TMDbClient {
	return &TMDbClient{apiKey: apiKey, baseURL: tmdbBaseURL, client: http.DefaultClient}
}

func (c *TMDbClient) get(ctx context.Context, path string, query url.Values, out any) error {
	query.Set("api_key", c.apiKey)
	reqURL := fmt.Sprintf("%s%s?%s", c.baseURL, path, query.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata: tmdb request to %s returned %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type tmdbSearchResponse[T any] struct {
	Results []T `json:"results"`
}

type tmdbMovieResult struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Overview     string `json:"overview"`
	ReleaseDate  string `json:"release_date"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

type tmdbTVResult struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	FirstAirDate string `json:"first_air_date"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

type tmdbVideosResponse struct {
	Results []struct {
		Site string `json:"site"`
		Type string `json:"type"`
		Key  string `json:"key"`
	} `json:"results"`
}

// SearchMovie returns the best-guess movie match for title (optionally
// narrowed by year), or nil if TMDb has nothing.
func (c *TMDbClient) SearchMovie(ctx context.Context, title string, year int) (*tmdbMovieResult, error) {
	q := url.Values{"query": {title}}
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	var resp tmdbSearchResponse[tmdbMovieResult]
	if err := c.get(ctx, "/search/movie", q, &resp); err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, nil
	}
	return &resp.Results[0], nil
}

func (c *TMDbClient) SearchTV(ctx context.Context, title string) (*tmdbTVResult, error) {
	q := url.Values{"query": {title}}
	var resp tmdbSearchResponse[tmdbTVResult]
	if err := c.get(ctx, "/search/tv", q, &resp); err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, nil
	}
	return &resp.Results[0], nil
}

// SearchResult is a single TMDb search hit, exposed for callers (like the
// content-request feature) that need to show a user several candidates to
// pick from -- unlike SearchMovie/SearchTV above, which only return TMDb's
// single best guess for automatic library matching.
type SearchResult struct {
	TmdbID      int
	Title       string
	Overview    string
	ReleaseDate string
	PosterURL   string
}

// DiscoverMovies returns every movie result TMDb has for query.
func (c *TMDbClient) DiscoverMovies(ctx context.Context, query string) ([]SearchResult, error) {
	var resp tmdbSearchResponse[tmdbMovieResult]
	if err := c.get(ctx, "/search/movie", url.Values{"query": {query}}, &resp); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, SearchResult{TmdbID: r.ID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterURL: imageURL(r.PosterPath)})
	}
	return out, nil
}

// DiscoverSeries returns every TV result TMDb has for query.
func (c *TMDbClient) DiscoverSeries(ctx context.Context, query string) ([]SearchResult, error) {
	var resp tmdbSearchResponse[tmdbTVResult]
	if err := c.get(ctx, "/search/tv", url.Values{"query": {query}}, &resp); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, SearchResult{TmdbID: r.ID, Title: r.Name, Overview: r.Overview, ReleaseDate: r.FirstAirDate, PosterURL: imageURL(r.PosterPath)})
	}
	return out, nil
}

// trailerURL fetches /movie/{id}/videos or /tv/{id}/videos and returns a
// YouTube watch URL for the first official trailer, if any.
func (c *TMDbClient) trailerURL(ctx context.Context, kind string, id int) (string, error) {
	var resp tmdbVideosResponse
	if err := c.get(ctx, fmt.Sprintf("/%s/%d/videos", kind, id), url.Values{}, &resp); err != nil {
		return "", err
	}
	for _, v := range resp.Results {
		if strings.EqualFold(v.Site, "YouTube") && strings.EqualFold(v.Type, "Trailer") {
			return "https://www.youtube.com/watch?v=" + v.Key, nil
		}
	}
	return "", nil
}

type tmdbExternalIDs struct {
	IMDbID string `json:"imdb_id"`
	TVDbID int    `json:"tvdb_id"` // only ever populated on the /tv external_ids response
}

// externalIDs fetches /{kind}/{id}/external_ids -- the same well-documented,
// stable TMDb endpoint Radarr/Sonarr use to cross-reference their own TMDb
// matches against IMDb/TheTVDB. Errors are the caller's to decide whether
// to ignore (this is enrichment, not the primary match).
func (c *TMDbClient) externalIDs(ctx context.Context, kind string, id int) (*tmdbExternalIDs, error) {
	var resp tmdbExternalIDs
	if err := c.get(ctx, fmt.Sprintf("/%s/%d/external_ids", kind, id), url.Values{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func imageURL(path string) string {
	if path == "" {
		return ""
	}
	return tmdbImageURL + path
}

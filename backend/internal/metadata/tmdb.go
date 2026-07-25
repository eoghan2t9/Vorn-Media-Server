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
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Overview     string  `json:"overview"`
	ReleaseDate  string  `json:"release_date"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	VoteAverage  float64 `json:"vote_average"`
}

type tmdbTVResult struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Overview     string  `json:"overview"`
	FirstAirDate string  `json:"first_air_date"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	VoteAverage  float64 `json:"vote_average"`
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
	Rating      float64
}

// DiscoverMovies returns every movie result TMDb has for query.
func (c *TMDbClient) DiscoverMovies(ctx context.Context, query string) ([]SearchResult, error) {
	var resp tmdbSearchResponse[tmdbMovieResult]
	if err := c.get(ctx, "/search/movie", url.Values{"query": {query}}, &resp); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, SearchResult{TmdbID: r.ID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterURL: imageURL(r.PosterPath), Rating: r.VoteAverage})
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
		out = append(out, SearchResult{TmdbID: r.ID, Title: r.Name, Overview: r.Overview, ReleaseDate: r.FirstAirDate, PosterURL: imageURL(r.PosterPath), Rating: r.VoteAverage})
	}
	return out, nil
}

// PagedResult is one page of a paginated TMDb listing (popular/trending),
// distinct from SearchResult's caller-facing shape only in that it also
// carries TMDb's own pagination bookkeeping so a browse UI knows whether
// there's a next page to load.
type PagedResult struct {
	Results    []SearchResult
	Page       int
	TotalPages int
}

type tmdbPagedResponse[T any] struct {
	Page         int `json:"page"`
	Results      []T `json:"results"`
	TotalPages   int `json:"total_pages"`
	TotalResults int `json:"total_results"`
}

func pageQuery(page int) url.Values {
	if page < 1 {
		page = 1
	}
	return url.Values{"page": {strconv.Itoa(page)}}
}

// PopularMovies returns one page of TMDb's currently popular movies, for a
// live catalog-browse view (no local persistence -- unlike DiscoverMovies,
// this isn't a text search, it's an always-fresh ranked listing).
func (c *TMDbClient) PopularMovies(ctx context.Context, page int) (PagedResult, error) {
	var resp tmdbPagedResponse[tmdbMovieResult]
	if err := c.get(ctx, "/movie/popular", pageQuery(page), &resp); err != nil {
		return PagedResult{}, err
	}
	out := PagedResult{Page: resp.Page, TotalPages: resp.TotalPages}
	for _, r := range resp.Results {
		out.Results = append(out.Results, SearchResult{TmdbID: r.ID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterURL: imageURL(r.PosterPath), Rating: r.VoteAverage})
	}
	return out, nil
}

// PopularSeries is PopularMovies' TV equivalent (GET /tv/popular).
func (c *TMDbClient) PopularSeries(ctx context.Context, page int) (PagedResult, error) {
	var resp tmdbPagedResponse[tmdbTVResult]
	if err := c.get(ctx, "/tv/popular", pageQuery(page), &resp); err != nil {
		return PagedResult{}, err
	}
	out := PagedResult{Page: resp.Page, TotalPages: resp.TotalPages}
	for _, r := range resp.Results {
		out.Results = append(out.Results, SearchResult{TmdbID: r.ID, Title: r.Name, Overview: r.Overview, ReleaseDate: r.FirstAirDate, PosterURL: imageURL(r.PosterPath), Rating: r.VoteAverage})
	}
	return out, nil
}

// TrendingMovies returns TMDb's trending-this-week movies (GET
// /trending/movie/week) -- a faster-moving, more "what's hot right now"
// counterpart to PopularMovies' steadier all-time-popularity ranking.
func (c *TMDbClient) TrendingMovies(ctx context.Context, page int) (PagedResult, error) {
	var resp tmdbPagedResponse[tmdbMovieResult]
	if err := c.get(ctx, "/trending/movie/week", pageQuery(page), &resp); err != nil {
		return PagedResult{}, err
	}
	out := PagedResult{Page: resp.Page, TotalPages: resp.TotalPages}
	for _, r := range resp.Results {
		out.Results = append(out.Results, SearchResult{TmdbID: r.ID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterURL: imageURL(r.PosterPath), Rating: r.VoteAverage})
	}
	return out, nil
}

// TrendingSeries is TrendingMovies' TV equivalent (GET /trending/tv/week).
func (c *TMDbClient) TrendingSeries(ctx context.Context, page int) (PagedResult, error) {
	var resp tmdbPagedResponse[tmdbTVResult]
	if err := c.get(ctx, "/trending/tv/week", pageQuery(page), &resp); err != nil {
		return PagedResult{}, err
	}
	out := PagedResult{Page: resp.Page, TotalPages: resp.TotalPages}
	for _, r := range resp.Results {
		out.Results = append(out.Results, SearchResult{TmdbID: r.ID, Title: r.Name, Overview: r.Overview, ReleaseDate: r.FirstAirDate, PosterURL: imageURL(r.PosterPath), Rating: r.VoteAverage})
	}
	return out, nil
}

// MovieDetails is the by-ID detail shape needed to materialize a local
// placeholder media_item from a browse card -- SearchMovie/DiscoverMovies
// only search by title, but a browse card already knows its exact TMDb ID.
type MovieDetails struct {
	TmdbID      int
	Title       string
	Overview    string
	ReleaseDate string
	PosterURL   string
	BackdropURL string
	// Runtime is TMDb's reported runtime in minutes, 0 if TMDb has none on
	// file. Used by acquisition's content-verification check (comparing
	// against a resolved release's actual probed duration) rather than
	// shown anywhere in the UI.
	Runtime int
	// ImdbID (e.g. "tt0137523"), empty if TMDb has none on file. Used to
	// query IMDb-ID-driven torrent indexers (see torrent.SearchByIMDb) --
	// not shown anywhere in the UI.
	ImdbID string
}

type tmdbMovieDetailsResult struct {
	ID           int             `json:"id"`
	Title        string          `json:"title"`
	Overview     string          `json:"overview"`
	ReleaseDate  string          `json:"release_date"`
	PosterPath   string          `json:"poster_path"`
	BackdropPath string          `json:"backdrop_path"`
	Runtime      int             `json:"runtime"`
	ExternalIDs  tmdbExternalIDs `json:"external_ids"`
}

// GetMovieDetails fetches GET /movie/{id}?append_to_response=external_ids --
// appending external_ids costs nothing extra (still one request) and is
// the only way to get IMDb ID, which TMDb doesn't include by default.
func (c *TMDbClient) GetMovieDetails(ctx context.Context, tmdbID int) (*MovieDetails, error) {
	var resp tmdbMovieDetailsResult
	query := url.Values{"append_to_response": {"external_ids"}}
	if err := c.get(ctx, fmt.Sprintf("/movie/%d", tmdbID), query, &resp); err != nil {
		return nil, err
	}
	return &MovieDetails{
		TmdbID: resp.ID, Title: resp.Title, Overview: resp.Overview, ReleaseDate: resp.ReleaseDate,
		PosterURL: imageURL(resp.PosterPath), BackdropURL: imageURL(resp.BackdropPath), Runtime: resp.Runtime,
		ImdbID: resp.ExternalIDs.IMDbID,
	}, nil
}

// SeasonSummary is one entry of SeriesDetails.Seasons -- just enough to
// know which season numbers exist and how many episodes to expect before
// fetching each season's own episode list via GetSeasonDetails.
type SeasonSummary struct {
	SeasonNumber int
	EpisodeCount int
}

// SeriesDetails is GetMovieDetails' TV equivalent, plus the season list
// needed to sync a placeholder series' episode tree.
type SeriesDetails struct {
	TmdbID       int
	Title        string
	Overview     string
	FirstAirDate string
	PosterURL    string
	BackdropURL  string
	Seasons      []SeasonSummary
	// ImdbID is the whole series' IMDb ID -- TV search (both TorBox's own
	// torrent-search API and IMDb generally) keys episodes off the series'
	// ID plus season/episode numbers, not a separate per-episode ID.
	ImdbID string
}

type tmdbSeriesDetailsResult struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	FirstAirDate string `json:"first_air_date"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
	Seasons      []struct {
		SeasonNumber int `json:"season_number"`
		EpisodeCount int `json:"episode_count"`
	} `json:"seasons"`
	ExternalIDs tmdbExternalIDs `json:"external_ids"`
}

// GetSeriesDetails fetches GET /tv/{id}?append_to_response=external_ids.
// Season 0 ("specials") is dropped -- specials aren't part of the regular
// episode-acquisition flow and would otherwise need special-casing
// everywhere a season number is used to build a search query.
func (c *TMDbClient) GetSeriesDetails(ctx context.Context, tmdbID int) (*SeriesDetails, error) {
	var resp tmdbSeriesDetailsResult
	query := url.Values{"append_to_response": {"external_ids"}}
	if err := c.get(ctx, fmt.Sprintf("/tv/%d", tmdbID), query, &resp); err != nil {
		return nil, err
	}
	out := &SeriesDetails{
		TmdbID: resp.ID, Title: resp.Name, Overview: resp.Overview, FirstAirDate: resp.FirstAirDate,
		PosterURL: imageURL(resp.PosterPath), BackdropURL: imageURL(resp.BackdropPath), ImdbID: resp.ExternalIDs.IMDbID,
	}
	for _, s := range resp.Seasons {
		if s.SeasonNumber == 0 {
			continue
		}
		out.Seasons = append(out.Seasons, SeasonSummary{SeasonNumber: s.SeasonNumber, EpisodeCount: s.EpisodeCount})
	}
	return out, nil
}

// EpisodeDetails is one episode entry from GetSeasonDetails.
type EpisodeDetails struct {
	SeasonNumber  int
	EpisodeNumber int
	Title         string
	Overview      string
	AirDate       string
	// Runtime is TMDb's reported episode runtime in minutes, 0 if TMDb has
	// none on file -- see MovieDetails.Runtime for what it's used for.
	Runtime int
}

type tmdbSeasonResult struct {
	Episodes []struct {
		SeasonNumber  int    `json:"season_number"`
		EpisodeNumber int    `json:"episode_number"`
		Name          string `json:"name"`
		Overview      string `json:"overview"`
		AirDate       string `json:"air_date"`
		Runtime       int    `json:"runtime"`
	} `json:"episodes"`
}

// GetSeasonDetails fetches GET /tv/{id}/season/{seasonNumber} for the
// season's full episode list.
func (c *TMDbClient) GetSeasonDetails(ctx context.Context, tmdbID, seasonNumber int) ([]EpisodeDetails, error) {
	var resp tmdbSeasonResult
	if err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", tmdbID, seasonNumber), url.Values{}, &resp); err != nil {
		return nil, err
	}
	out := make([]EpisodeDetails, 0, len(resp.Episodes))
	for _, e := range resp.Episodes {
		out = append(out, EpisodeDetails{
			SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, Runtime: e.Runtime,
			Title: e.Name, Overview: e.Overview, AirDate: e.AirDate,
		})
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

type tmdbCreditsResponse struct {
	Cast []struct {
		Name        string `json:"name"`
		Character   string `json:"character"`
		ProfilePath string `json:"profile_path"`
	} `json:"cast"`
	Crew []struct {
		Name string `json:"name"`
		Job  string `json:"job"`
	} `json:"crew"`
}

// CastMember is one billed actor, capped to castLimit by TMDb's own
// order-of-billing (the /credits response already comes pre-sorted).
type CastMember struct {
	Name      string
	Character string
	PhotoURL  string
}

const castLimit = 12

// credits fetches /{kind}/{id}/credits and returns the top castLimit-billed
// cast plus every crew member whose job is "Director".
func (c *TMDbClient) credits(ctx context.Context, kind string, id int) ([]CastMember, []string, error) {
	var resp tmdbCreditsResponse
	if err := c.get(ctx, fmt.Sprintf("/%s/%d/credits", kind, id), url.Values{}, &resp); err != nil {
		return nil, nil, err
	}
	cast := make([]CastMember, 0, castLimit)
	for i, m := range resp.Cast {
		if i >= castLimit {
			break
		}
		cast = append(cast, CastMember{Name: m.Name, Character: m.Character, PhotoURL: imageURL(m.ProfilePath)})
	}
	var directors []string
	for _, m := range resp.Crew {
		if m.Job == "Director" {
			directors = append(directors, m.Name)
		}
	}
	return cast, directors, nil
}

const similarLimit = 12

// similar fetches /{kind}/{id}/similar ("movie" or "tv"), capped to
// similarLimit -- reuses SearchResult, the same shape DiscoverMovies/
// PopularMovies already produce for a title hit.
func (c *TMDbClient) similar(ctx context.Context, kind string, id int) ([]SearchResult, error) {
	if kind == "movie" {
		var resp tmdbPagedResponse[tmdbMovieResult]
		if err := c.get(ctx, fmt.Sprintf("/movie/%d/similar", id), url.Values{}, &resp); err != nil {
			return nil, err
		}
		out := make([]SearchResult, 0, similarLimit)
		for i, r := range resp.Results {
			if i >= similarLimit {
				break
			}
			out = append(out, SearchResult{TmdbID: r.ID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterURL: imageURL(r.PosterPath), Rating: r.VoteAverage})
		}
		return out, nil
	}
	var resp tmdbPagedResponse[tmdbTVResult]
	if err := c.get(ctx, fmt.Sprintf("/tv/%d/similar", id), url.Values{}, &resp); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, similarLimit)
	for i, r := range resp.Results {
		if i >= similarLimit {
			break
		}
		out = append(out, SearchResult{TmdbID: r.ID, Title: r.Name, Overview: r.Overview, ReleaseDate: r.FirstAirDate, PosterURL: imageURL(r.PosterPath), Rating: r.VoteAverage})
	}
	return out, nil
}

type tmdbRatingResponse struct {
	VoteAverage float64 `json:"vote_average"`
}

// rating fetches /{kind}/{id} for just its vote_average -- used only by the
// cast-backfill path (sync.go), which has an id but no fresh search result
// to read a rating off of the way MatchMovie/MatchSeries already can.
func (c *TMDbClient) rating(ctx context.Context, kind string, id int) (float64, error) {
	var resp tmdbRatingResponse
	if err := c.get(ctx, fmt.Sprintf("/%s/%d", kind, id), url.Values{}, &resp); err != nil {
		return 0, err
	}
	return resp.VoteAverage, nil
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

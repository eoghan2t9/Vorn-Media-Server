package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
)

const tvdbBaseURL = "https://api4.thetvdb.com/v4"

// TVDbClient is a thin wrapper over TheTVDB v4 API
// (https://thetvdb.github.io/v4-api/): login once for a ~month-long JWT,
// then Bearer-auth every subsequent request, re-logging in once if the
// cached token has expired server-side.
type TVDbClient struct {
	apiKey string
	// pin is only required for a "user-support" API key tied to an
	// individual paid subscriber account, per TheTVDB's own auth docs --
	// a standard project key (the expected case) omits it entirely.
	pin     string
	baseURL string
	client  httpDoer

	mu    sync.Mutex
	token string
}

func NewTVDbClient(apiKey, pin string) *TVDbClient {
	return &TVDbClient{apiKey: apiKey, pin: pin, baseURL: tvdbBaseURL, client: http.DefaultClient}
}

type tvdbLoginResponse struct {
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
}

func (c *TVDbClient) login(ctx context.Context) error {
	body := map[string]string{"apikey": c.apiKey}
	if c.pin != "" {
		body["pin"] = c.pin
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/login", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata: thetvdb login returned %d", resp.StatusCode)
	}
	var out tvdbLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	c.mu.Lock()
	c.token = out.Data.Token
	c.mu.Unlock()
	return nil
}

func (c *TVDbClient) cachedToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *TVDbClient) clearToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// get issues an authenticated GET, logging in first if there's no cached
// token yet and retrying once (with a fresh login) on a 401 -- the token
// is valid for about a month, long enough that a running Vorn process
// will rarely if ever need the retry path, but a restart-less month
// boundary shouldn't hard-fail every subsequent series match.
func (c *TVDbClient) get(ctx context.Context, path string, query url.Values, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		token := c.cachedToken()
		if token == "" {
			if err := c.login(ctx); err != nil {
				return err
			}
			token = c.cachedToken()
		}

		reqURL := c.baseURL + path
		if len(query) > 0 {
			reqURL += "?" + query.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := c.client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			c.clearToken()
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("metadata: thetvdb request to %s returned %d", path, resp.StatusCode)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return fmt.Errorf("metadata: thetvdb: unauthorized even after re-login")
}

// tvdbSearchResult is the subset of TheTVDB's search result fields Vorn
// needs. tvdb_id is the plain numeric TheTVDB ID as a string -- distinct
// from "id"/"objectID", which are compound/search-engine-internal values
// not meant to be used for other endpoint lookups (confirmed against the
// v4 API's own OpenAPI spec).
type tvdbSearchResult struct {
	TVDbID       string `json:"tvdb_id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Year         string `json:"year"`
	FirstAirTime string `json:"first_air_time"`
	ImageURL     string `json:"image_url"`
}

type tvdbSearchResponse struct {
	Data []tvdbSearchResult `json:"data"`
}

// SearchSeries returns the best-guess series match, or nil if TheTVDB has
// nothing.
func (c *TVDbClient) SearchSeries(ctx context.Context, title string) (*tvdbSearchResult, error) {
	var resp tvdbSearchResponse
	q := url.Values{"query": {title}, "type": {"series"}}
	if err := c.get(ctx, "/search", q, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, nil
	}
	return &resp.Data[0], nil
}

// TVDbProvider implements SeriesProvider (not the full Provider interface
// -- TheTVDB is used here only as a fallback series matcher, never for
// movies) against TheTVDB v4 API.
type TVDbProvider struct {
	client *TVDbClient
}

func NewTVDbProvider(apiKey, pin string) *TVDbProvider {
	return &TVDbProvider{client: NewTVDbClient(apiKey, pin)}
}

func (p *TVDbProvider) MatchSeries(ctx context.Context, title string) (*Match, error) {
	result, err := p.client.SearchSeries(ctx, title)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	releaseDate := result.FirstAirTime
	if releaseDate == "" && result.Year != "" {
		releaseDate = result.Year + "-01-01"
	}
	tvdbID, _ := strconv.Atoi(result.TVDbID)

	return &Match{
		Title:       result.Name,
		Overview:    result.Overview,
		ReleaseDate: releaseDate,
		PosterURL:   result.ImageURL,
		TVDbID:      tvdbID,
	}, nil
}

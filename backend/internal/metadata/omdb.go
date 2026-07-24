package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const omdbBaseURL = "http://www.omdbapi.com/"

type omdbResponse struct {
	Response string `json:"Response"` // "True" or "False" -- always check this, not HTTP status, per OMDb's own convention
	Error    string `json:"Error"`
	Ratings  []struct {
		Source string `json:"Source"`
		Value  string `json:"Value"`
	} `json:"Ratings"`
}

// OMDbClient fetches IMDb/Rotten Tomatoes ratings from OMDb
// (https://www.omdbapi.com/) by IMDb ID. Pure ratings enrichment layered
// onto an existing match -- OMDb has no useful movie/TV search of its own
// for Vorn's purposes, and needs an IMDb ID (from TMDb's external_ids) as
// input rather than producing a match itself.
type OMDbClient struct {
	apiKey  string
	baseURL string
	client  httpDoer
}

func NewOMDbClient(apiKey string) *OMDbClient {
	return &OMDbClient{apiKey: apiKey, baseURL: omdbBaseURL, client: http.DefaultClient}
}

// RatingsByIMDbID returns the IMDb rating (e.g. "8.8") and Rotten Tomatoes
// rating (e.g. "94%"), whichever OMDb has -- either may come back empty
// with no error if OMDb simply doesn't have that source for this title.
func (c *OMDbClient) RatingsByIMDbID(ctx context.Context, imdbID string) (imdbRating, rottenTomatoes string, err error) {
	q := url.Values{"i": {imdbID}, "apikey": {c.apiKey}}
	reqURL := c.baseURL + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("metadata: omdb request returned %d", resp.StatusCode)
	}
	var out omdbResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	if out.Response != "True" {
		return "", "", fmt.Errorf("metadata: omdb: %s", out.Error)
	}
	for _, r := range out.Ratings {
		switch r.Source {
		case "Internet Movie Database":
			imdbRating = r.Value
		case "Rotten Tomatoes":
			rottenTomatoes = r.Value
		}
	}
	return imdbRating, rottenTomatoes, nil
}

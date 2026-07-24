package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const fanartBaseURL = "http://webservice.fanart.tv/v3"

// fanartArt is the shape shared by every art-type array in a Fanart.tv
// response (moviebackground, hdmovielogo, tvposter, ...): lang is "00" for
// textless/international art, a real language code otherwise.
type fanartArt struct {
	URL  string `json:"url"`
	Lang string `json:"lang"`
}

// pickArt prefers English art, then textless ("00"), then whatever's
// first -- English reads best for Vorn's own UI, textless is the next-best
// fallback since it at least won't have a foreign-language logo baked in.
func pickArt(list []fanartArt) string {
	if len(list) == 0 {
		return ""
	}
	for _, a := range list {
		if a.Lang == "en" {
			return a.URL
		}
	}
	for _, a := range list {
		if a.Lang == "00" {
			return a.URL
		}
	}
	return list[0].URL
}

type fanartMovieResponse struct {
	MoviePoster     []fanartArt `json:"movieposter"`
	MovieBackground []fanartArt `json:"moviebackground"`
	HDMovieLogo     []fanartArt `json:"hdmovielogo"`
}

type fanartTVResponse struct {
	TVPoster       []fanartArt `json:"tvposter"`
	ShowBackground []fanartArt `json:"showbackground"`
	HDTVLogo       []fanartArt `json:"hdtvlogo"`
}

// FanartClient fetches supplemental artwork from Fanart.tv
// (https://fanart.tv/api-docs/api-v3/) -- higher-resolution posters/
// backdrops than TMDb typically has, plus clear logos TMDb doesn't provide
// at all. It's pure enrichment layered onto an existing TMDb/TheTVDB match,
// not a matcher itself: both endpoints are keyed by an ID from another
// provider (TMDb ID for movies, TheTVDB ID for TV), not a title search.
type FanartClient struct {
	apiKey  string
	baseURL string
	client  httpDoer
}

func NewFanartClient(apiKey string) *FanartClient {
	return &FanartClient{apiKey: apiKey, baseURL: fanartBaseURL, client: http.DefaultClient}
}

func (c *FanartClient) get(ctx context.Context, path string, out any) error {
	reqURL := fmt.Sprintf("%s%s?api_key=%s", c.baseURL, path, url.QueryEscape(c.apiKey))
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
		return fmt.Errorf("metadata: fanart.tv request to %s returned %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// MovieArt fetches poster/backdrop/logo art for a movie by its TMDb ID.
// Returns empty strings (no error) for any art type Fanart.tv doesn't have.
func (c *FanartClient) MovieArt(ctx context.Context, tmdbID int) (poster, backdrop, logo string, err error) {
	var resp fanartMovieResponse
	if err := c.get(ctx, fmt.Sprintf("/movies/%d", tmdbID), &resp); err != nil {
		return "", "", "", err
	}
	return pickArt(resp.MoviePoster), pickArt(resp.MovieBackground), pickArt(resp.HDMovieLogo), nil
}

// SeriesArt fetches poster/backdrop/logo art for a TV series by its
// TheTVDB ID -- Fanart.tv's TV endpoint has no TMDb-ID variant, so this is
// only usable when a match's TVDbID was populated (via TMDb's own
// external_ids, or a TheTVDB fallback match).
func (c *FanartClient) SeriesArt(ctx context.Context, tvdbID int) (poster, backdrop, logo string, err error) {
	var resp fanartTVResponse
	if err := c.get(ctx, fmt.Sprintf("/tv/%d", tvdbID), &resp); err != nil {
		return "", "", "", err
	}
	return pickArt(resp.TVPoster), pickArt(resp.ShowBackground), pickArt(resp.HDTVLogo), nil
}

package nzb

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SearchResult struct {
	IndexerName string
	Title       string
	SizeBytes   int64
	DownloadURL string // URL to fetch the .nzb file from
	PublishedAt time.Time
}

// newznabFeed models the subset of the Newznab response format (an RSS 2.0
// feed with a custom "newznab:attr" namespace for metadata like size and
// category) that Vorn cares about. Newznab is the standard search API
// protocol spoken by hosted Usenet indexer sites like NZBGeek -- Torznab
// (which Vorn already speaks for torrent indexers) is itself an extension
// of this same shape, so the parsing logic mirrors torrent/indexer.go.
type newznabFeed struct {
	Channel struct {
		Items []newznabItem `xml:"item"`
	} `xml:"channel"`
}

type newznabItem struct {
	Title     string `xml:"title"`
	Link      string `xml:"link"`
	PubDate   string `xml:"pubDate"`
	Enclosure struct {
		URL string `xml:"url,attr"`
	} `xml:"enclosure"`
	Attrs []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

func (it newznabItem) attr(name string) string {
	for _, a := range it.Attrs {
		if a.Name == name {
			return a.Value
		}
	}
	return ""
}

// newznabError models the standard Newznab XML error response
// (<error code="..." description="..."/>), which indexers return with an
// HTTP 200 status even on a bad API key -- checking the status code alone
// isn't enough to detect a failure.
type newznabError struct {
	XMLName     xml.Name `xml:"error"`
	Code        string   `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

// IndexerCapabilities reports whether an indexer's t=caps document
// advertises support for id-based search -- the only kind of search Vorn's
// acquisition system actually uses (see resolveImdbSearchParams in
// acquisition/service.go), so an indexer supporting neither is useless to
// Vorn even if free-text search works fine.
type IndexerCapabilities struct {
	SupportsImdb bool
	SupportsTvdb bool
}

// newznabCaps models the subset of the Newznab capabilities document
// (t=caps) that matters for IndexerCapabilities: whether imdbid/tvdbid
// appear in either search mode's supportedParams attribute.
type newznabCaps struct {
	Searching struct {
		MovieSearch struct {
			SupportedParams string `xml:"supportedParams,attr"`
		} `xml:"movie-search"`
		TVSearch struct {
			SupportedParams string `xml:"supportedParams,attr"`
		} `xml:"tv-search"`
	} `xml:"searching"`
}

func parseIndexerCapabilities(body []byte) IndexerCapabilities {
	var caps newznabCaps
	if xml.Unmarshal(body, &caps) != nil {
		return IndexerCapabilities{}
	}
	params := caps.Searching.MovieSearch.SupportedParams + "," + caps.Searching.TVSearch.SupportedParams
	return IndexerCapabilities{
		SupportsImdb: strings.Contains(params, "imdbid"),
		SupportsTvdb: strings.Contains(params, "tvdbid"),
	}
}

// TestIndexer verifies a Newznab indexer's base URL and API key by
// requesting its capabilities document (t=caps) -- the standard way to
// check connectivity/auth without running a real search -- and reports
// which id-based search params (imdbid/tvdbid) it actually supports, so
// callers can gate on Vorn only ever using id-based search
// (resolveImdbSearchParams).
func TestIndexer(ctx context.Context, baseURL, apiKey string) (*IndexerCapabilities, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api")
	if err != nil {
		return nil, fmt.Errorf("nzb: parsing indexer URL: %w", err)
	}
	q := u.Query()
	q.Set("t", "caps")
	if apiKey != "" {
		q.Set("apikey", apiKey)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nzb: contacting indexer: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("nzb: reading indexer response: %w", err)
	}

	var newznabErr newznabError
	if xml.Unmarshal(body, &newznabErr) == nil && newznabErr.Description != "" {
		return nil, fmt.Errorf("nzb: indexer rejected request: %s", newznabErr.Description)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nzb: indexer returned status %d", resp.StatusCode)
	}
	caps := parseIndexerCapabilities(body)
	return &caps, nil
}

// SearchIndexer queries a single Newznab-compatible indexer for a title.
func SearchIndexer(ctx context.Context, name, baseURL, apiKey, query string) ([]SearchResult, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api")
	if err != nil {
		return nil, fmt.Errorf("nzb: parsing indexer URL: %w", err)
	}
	q := u.Query()
	q.Set("t", "search")
	q.Set("q", query)
	if apiKey != "" {
		q.Set("apikey", apiKey)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nzb: querying indexer %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nzb: indexer %s returned status %d", name, resp.StatusCode)
	}

	var feed newznabFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("nzb: decoding indexer %s response: %w", name, err)
	}

	out := make([]SearchResult, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		size, _ := strconv.ParseInt(it.attr("size"), 10, 64)
		published, _ := time.Parse(time.RFC1123Z, it.PubDate)

		downloadURL := it.Enclosure.URL
		if downloadURL == "" {
			downloadURL = it.Link
		}

		out = append(out, SearchResult{
			IndexerName: name,
			Title:       it.Title,
			SizeBytes:   size,
			DownloadURL: downloadURL,
			PublishedAt: published,
		})
	}
	return out, nil
}

// SearchIndexerByIMDb queries a single Newznab-compatible indexer using its
// id-based search functions (t=movie / t=tvsearch) instead of the generic
// free-text t=search SearchIndexer uses -- both are part of the same
// standard Newznab spec (the same protocol Sonarr/Radarr/NZBHydra use this
// exact way), so this needs no new indexer type/config, just a different
// query mode against the same base URL/API key. A query is treated as "TV"
// (t=tvsearch) whenever a season is given or a tvdbID is known (TVDB has
// no concept of movies, so a bare tvdbID with no season is still a TV
// query, e.g. a general show search), "movie" (t=movie, imdbid only --
// movies have no TVDB id) otherwise. Both imdbid and tvdbid are sent for
// tv-search when known: real indexers' supported tv-search params vary
// (confirmed against a live NZBGeek account, whose tv-search doesn't
// accept imdbid at all -- only tvdbid/rid/tvmazeid), and sending an id an
// indexer doesn't support for a given mode is harmless, so sending both
// maximizes compatibility without needing per-indexer capability
// detection. Indexers that don't actually support these functions
// typically just return an empty result set or a Newznab error, both
// already handled the same as any other SearchIndexer failure by the
// caller.
func SearchIndexerByIMDb(ctx context.Context, name, baseURL, apiKey, imdbID, tvdbID string, season, episode int) ([]SearchResult, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api")
	if err != nil {
		return nil, fmt.Errorf("nzb: parsing indexer URL: %w", err)
	}
	q := u.Query()
	if imdbID != "" {
		q.Set("imdbid", strings.TrimPrefix(imdbID, "tt"))
	}
	if season > 0 || tvdbID != "" {
		q.Set("t", "tvsearch")
		if season > 0 {
			q.Set("season", strconv.Itoa(season))
			if episode > 0 {
				q.Set("ep", strconv.Itoa(episode))
			}
		}
		if tvdbID != "" {
			q.Set("tvdbid", tvdbID)
		}
	} else {
		q.Set("t", "movie")
	}
	if apiKey != "" {
		q.Set("apikey", apiKey)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nzb: querying indexer %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nzb: indexer %s returned status %d", name, resp.StatusCode)
	}

	var feed newznabFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("nzb: decoding indexer %s response: %w", name, err)
	}

	out := make([]SearchResult, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		size, _ := strconv.ParseInt(it.attr("size"), 10, 64)
		published, _ := time.Parse(time.RFC1123Z, it.PubDate)

		downloadURL := it.Enclosure.URL
		if downloadURL == "" {
			downloadURL = it.Link
		}

		out = append(out, SearchResult{
			IndexerName: name,
			Title:       it.Title,
			SizeBytes:   size,
			DownloadURL: downloadURL,
			PublishedAt: published,
		})
	}
	return out, nil
}

// FetchNZB downloads the raw .nzb file body from a search result's
// DownloadURL (already a complete, indexer-authenticated URL, same as a
// Torznab enclosure link).
func FetchNZB(ctx context.Context, downloadURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nzb: fetching nzb file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nzb: fetching nzb file returned status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("nzb: reading nzb file: %w", err)
	}
	return data, nil
}

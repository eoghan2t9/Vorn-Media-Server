package torrent

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
	Seeders     int
	Peers       int
	DownloadURL string // magnet: URI or a .torrent file URL
	PublishedAt time.Time
}

// torznabFeed models the subset of the Torznab response format (an RSS 2.0
// feed with a custom "torznab:attr" namespace for metadata like seeders and
// size) that Vorn cares about. Torznab is a widely adopted, provider-
// agnostic indexer protocol — the same one Prowlarr/Sonarr/Radarr speak —
// so any Torznab-compatible indexer works here without Vorn depending on
// those projects.
type torznabFeed struct {
	Channel struct {
		Items []torznabItem `xml:"item"`
	} `xml:"channel"`
}

type torznabItem struct {
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

func (it torznabItem) attr(name string) string {
	for _, a := range it.Attrs {
		if a.Name == name {
			return a.Value
		}
	}
	return ""
}

// torznabError models the standard Newznab/Torznab XML error response
// (<error code="..." description="..."/>), which several indexers return
// with an HTTP 200 status even on a bad API key -- checking the status
// code alone isn't enough to detect a failure.
type torznabError struct {
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

// torznabCaps models the subset of the Torznab/Newznab capabilities
// document (t=caps) that matters for IndexerCapabilities: whether imdbid/
// tvdbid appear in either search mode's supportedParams attribute.
type torznabCaps struct {
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
	var caps torznabCaps
	if xml.Unmarshal(body, &caps) != nil {
		return IndexerCapabilities{}
	}
	params := caps.Searching.MovieSearch.SupportedParams + "," + caps.Searching.TVSearch.SupportedParams
	return IndexerCapabilities{
		SupportsImdb: strings.Contains(params, "imdbid"),
		SupportsTvdb: strings.Contains(params, "tvdbid"),
	}
}

// TestIndexer verifies a Torznab indexer's base URL and API key by
// requesting its capabilities document (t=caps) -- the standard Torznab way
// to check connectivity/auth without running a real search -- and reports
// which id-based search params (imdbid/tvdbid) it actually supports, so
// callers can gate on Vorn only ever using id-based search
// (resolveImdbSearchParams).
func TestIndexer(ctx context.Context, baseURL, apiKey string) (*IndexerCapabilities, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api")
	if err != nil {
		return nil, fmt.Errorf("torrent: parsing indexer URL: %w", err)
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
		return nil, fmt.Errorf("torrent: contacting indexer: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("torrent: reading indexer response: %w", err)
	}

	var torznabErr torznabError
	if xml.Unmarshal(body, &torznabErr) == nil && torznabErr.Description != "" {
		return nil, fmt.Errorf("torrent: indexer rejected request: %s", torznabErr.Description)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("torrent: indexer returned status %d", resp.StatusCode)
	}
	caps := parseIndexerCapabilities(body)
	return &caps, nil
}

// SearchIndexer queries a single Torznab-compatible indexer for a title.
func SearchIndexer(ctx context.Context, name, baseURL, apiKey, query string) ([]SearchResult, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api")
	if err != nil {
		return nil, fmt.Errorf("torrent: parsing indexer URL: %w", err)
	}
	q := u.Query()
	q.Set("t", "search")
	q.Set("q", query)
	if apiKey != "" {
		q.Set("apikey", apiKey)
	}
	u.RawQuery = q.Encode()

	return doTorznabRequest(ctx, name, u.String())
}

// SearchIndexerByIMDb queries a single Torznab-compatible indexer using its
// id-based search functions (t=movie / t=tvsearch) instead of the generic
// free-text t=search SearchIndexer uses -- Torznab is the torrent-side
// sibling of the Newznab protocol nzb.SearchIndexerByIMDb already speaks
// this same way, including the same "send both imdbid and tvdbid whenever
// known" reasoning: which id an indexer's tv-search function actually
// supports varies (a real Newznab account this codebase already tested
// against doesn't accept imdbid for tv-search at all, only tvdbid), and an
// indexer ignoring an id it doesn't support for a given mode is harmless.
// A query is treated as "TV" (t=tvsearch) whenever a season is given or a
// tvdbID is known (TVDB has no concept of movies), "movie" (t=movie,
// imdbid only) otherwise.
func SearchIndexerByIMDb(ctx context.Context, name, baseURL, apiKey, imdbID, tvdbID string, season, episode int) ([]SearchResult, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api")
	if err != nil {
		return nil, fmt.Errorf("torrent: parsing indexer URL: %w", err)
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

	return doTorznabRequest(ctx, name, u.String())
}

func doTorznabRequest(ctx context.Context, name, requestURL string) ([]SearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("torrent: querying indexer %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("torrent: indexer %s returned status %d", name, resp.StatusCode)
	}

	var feed torznabFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("torrent: decoding indexer %s response: %w", name, err)
	}

	out := make([]SearchResult, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		size, _ := strconv.ParseInt(it.attr("size"), 10, 64)
		seeders, _ := strconv.Atoi(it.attr("seeders"))
		peers, _ := strconv.Atoi(it.attr("peers"))
		published, _ := time.Parse(time.RFC1123Z, it.PubDate)

		downloadURL := it.Enclosure.URL
		if downloadURL == "" {
			downloadURL = it.Link
		}

		out = append(out, SearchResult{
			IndexerName: name,
			Title:       it.Title,
			SizeBytes:   size,
			Seeders:     seeders,
			Peers:       peers,
			DownloadURL: downloadURL,
			PublishedAt: published,
		})
	}
	return out, nil
}

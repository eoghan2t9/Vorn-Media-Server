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

// TestIndexer verifies a Torznab indexer's base URL and API key by
// requesting its capabilities document (t=caps) -- the standard Torznab
// way to check connectivity/auth without running a real search.
func TestIndexer(ctx context.Context, baseURL, apiKey string) error {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api")
	if err != nil {
		return fmt.Errorf("torrent: parsing indexer URL: %w", err)
	}
	q := u.Query()
	q.Set("t", "caps")
	if apiKey != "" {
		q.Set("apikey", apiKey)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("torrent: contacting indexer: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("torrent: reading indexer response: %w", err)
	}

	var torznabErr torznabError
	if xml.Unmarshal(body, &torznabErr) == nil && torznabErr.Description != "" {
		return fmt.Errorf("torrent: indexer rejected request: %s", torznabErr.Description)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("torrent: indexer returned status %d", resp.StatusCode)
	}
	return nil
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
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

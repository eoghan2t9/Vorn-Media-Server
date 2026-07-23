package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const (
	openLibraryBaseURL   = "https://openlibrary.org"
	openLibraryCoversURL = "https://covers.openlibrary.org"
	bookUserAgent        = "Vorn/1.0 (+https://github.com/eoghan2t9/Vorn-Media-Server)"
)

// BookMatch is what AudiobookProvider returns for a title/author lookup.
type BookMatch struct {
	WorkKey     string // e.g. "/works/OL12345W"
	Title       string
	Author      string
	ReleaseDate string // publish year as YYYY, may be empty
	PosterURL   string // empty if Open Library has no cover on file
}

// AudiobookProvider looks up book metadata against an external source.
// OpenLibraryProvider is the only implementation -- Audible has no public
// API, so using it would mean unofficial scraping against their ToS.
type AudiobookProvider interface {
	MatchBook(ctx context.Context, title, author string) (*BookMatch, error)
}

// OpenLibraryProvider's baseURL/coversBaseURL are struct fields rather than
// using the package consts directly -- same reasoning as TMDbClient -- so
// tests can point them at an httptest server.
type OpenLibraryProvider struct {
	baseURL       string
	coversBaseURL string
	client        httpDoer
}

func NewOpenLibraryProvider() *OpenLibraryProvider {
	return &OpenLibraryProvider{baseURL: openLibraryBaseURL, coversBaseURL: openLibraryCoversURL, client: http.DefaultClient}
}

type olSearchResponse struct {
	Docs []olDoc `json:"docs"`
}

type olDoc struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	AuthorName       []string `json:"author_name"`
	FirstPublishYear int      `json:"first_publish_year"`
	CoverID          int      `json:"cover_i"`
}

func (p *OpenLibraryProvider) MatchBook(ctx context.Context, title, author string) (*BookMatch, error) {
	q := url.Values{"title": {title}, "limit": {"1"}}
	if author != "" && author != "Unknown Artist" {
		q.Set("author", author)
	}
	reqURL := fmt.Sprintf("%s/search.json?%s", p.baseURL, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", bookUserAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata: open library request to %s returned %d", reqURL, resp.StatusCode)
	}

	var out olSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Docs) == 0 {
		return nil, nil
	}
	doc := out.Docs[0]

	match := &BookMatch{
		WorkKey: doc.Key,
		Title:   doc.Title,
		Author:  author,
	}
	if len(doc.AuthorName) > 0 {
		match.Author = doc.AuthorName[0]
	}
	if doc.FirstPublishYear > 0 {
		match.ReleaseDate = fmt.Sprintf("%04d", doc.FirstPublishYear)
	}
	if doc.CoverID > 0 {
		match.PosterURL = fmt.Sprintf("%s/b/id/%d-L.jpg", p.coversBaseURL, doc.CoverID)
	}
	return match, nil
}

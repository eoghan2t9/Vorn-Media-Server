package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const (
	musicbrainzBaseURL = "https://musicbrainz.org/ws/2"
	coverArtBaseURL    = "https://coverartarchive.org"
	// MusicBrainz's API usage policy requires a descriptive User-Agent
	// identifying the application and a contact point -- requests without
	// one are more aggressively rate-limited. This never needs to be
	// admin-configured, unlike an API key.
	musicUserAgent = "Vorn/1.0 (+https://github.com/eoghan2t9/Vorn-Media-Server)"
)

// MusicMatch is what MusicBrainzProvider returns for an album lookup.
type MusicMatch struct {
	ReleaseMBID string
	Title       string
	ArtistName  string
	ReleaseDate string // YYYY[-MM[-DD]], may be partial or empty
	PosterURL   string // Cover Art Archive URL, empty if no art on file
}

// MusicProvider looks up album releases against an external music metadata
// source. MusicBrainzProvider is the only implementation.
type MusicProvider interface {
	MatchAlbum(ctx context.Context, artist, album string) (*MusicMatch, error)
}

// MusicBrainzProvider talks to MusicBrainz (release search) and Cover Art
// Archive (front cover lookup). baseURL/coverArtBaseURL are struct fields
// rather than using the package consts directly -- same reasoning as
// TMDbClient -- so tests can point them at an httptest server.
type MusicBrainzProvider struct {
	baseURL        string
	coverArtBaseURL string
	client         httpDoer
}

func NewMusicBrainzProvider() *MusicBrainzProvider {
	return &MusicBrainzProvider{baseURL: musicbrainzBaseURL, coverArtBaseURL: coverArtBaseURL, client: http.DefaultClient}
}

type mbReleaseSearchResponse struct {
	Releases []mbRelease `json:"releases"`
}

type mbRelease struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Date         string `json:"date"`
	ArtistCredit []struct {
		Name string `json:"name"`
	} `json:"artist-credit"`
}

type mbCoverArtInfo struct {
	Images []struct {
		Front bool `json:"front"`
	} `json:"images"`
}

func (p *MusicBrainzProvider) MatchAlbum(ctx context.Context, artist, album string) (*MusicMatch, error) {
	q := url.Values{
		"query": {fmt.Sprintf("artist:%s AND release:%s", artist, album)},
		"fmt":   {"json"},
		"limit": {"1"},
	}
	reqURL := fmt.Sprintf("%s/release/?%s", p.baseURL, q.Encode())

	var resp mbReleaseSearchResponse
	if err := p.get(ctx, reqURL, &resp); err != nil {
		return nil, err
	}
	if len(resp.Releases) == 0 {
		return nil, nil
	}
	release := resp.Releases[0]

	artistName := artist
	if len(release.ArtistCredit) > 0 && release.ArtistCredit[0].Name != "" {
		artistName = release.ArtistCredit[0].Name
	}

	match := &MusicMatch{
		ReleaseMBID: release.ID,
		Title:       release.Title,
		ArtistName:  artistName,
		ReleaseDate: release.Date,
	}
	poster, err := p.coverArtURL(ctx, release.ID)
	if err != nil {
		return nil, err
	}
	match.PosterURL = poster
	return match, nil
}

// coverArtURL checks whether Cover Art Archive actually has a front cover
// for releaseMBID before returning its URL, so callers never store a link
// to a 404 (Cover Art Archive doesn't host art for every release) -- a
// missing-art 404 here is a normal, expected outcome, not a failure.
func (p *MusicBrainzProvider) coverArtURL(ctx context.Context, releaseMBID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.coverArtBaseURL+"/release/"+releaseMBID, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", musicUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata: cover art archive request for %s returned %d", releaseMBID, resp.StatusCode)
	}

	var info mbCoverArtInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	for _, img := range info.Images {
		if img.Front {
			return p.coverArtBaseURL + "/release/" + releaseMBID + "/front", nil
		}
	}
	return "", nil
}

func (p *MusicBrainzProvider) get(ctx context.Context, reqURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", musicUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata: musicbrainz request to %s returned %d", reqURL, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

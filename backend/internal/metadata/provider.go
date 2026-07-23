package metadata

import "context"

// Match is what any metadata provider returns for a title, normalized so the
// sync job doesn't need to know which provider produced it.
type Match struct {
	ProviderID  int
	Title       string
	Overview    string
	ReleaseDate string // YYYY-MM-DD, may be partial or empty
	PosterURL   string
	BackdropURL string
	TrailerURL  string
}

// Provider looks up movies and TV series against an external metadata
// source. TMDbProvider is the only implementation today; the interface
// exists so a second source (Fanart.tv, OMDb, ...) can be added later
// without touching the sync job.
type Provider interface {
	MatchMovie(ctx context.Context, title string, year int) (*Match, error)
	MatchSeries(ctx context.Context, title string) (*Match, error)
}

type TMDbProvider struct {
	client *TMDbClient
}

func NewTMDbProvider(apiKey string) *TMDbProvider {
	return &TMDbProvider{client: NewTMDbClient(apiKey)}
}

func (p *TMDbProvider) MatchMovie(ctx context.Context, title string, year int) (*Match, error) {
	result, err := p.client.SearchMovie(ctx, title, year)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	trailer, _ := p.client.trailerURL(ctx, "movie", result.ID)
	return &Match{
		ProviderID:  result.ID,
		Title:       result.Title,
		Overview:    result.Overview,
		ReleaseDate: result.ReleaseDate,
		PosterURL:   imageURL(result.PosterPath),
		BackdropURL: imageURL(result.BackdropPath),
		TrailerURL:  trailer,
	}, nil
}

func (p *TMDbProvider) MatchSeries(ctx context.Context, title string) (*Match, error) {
	result, err := p.client.SearchTV(ctx, title)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	trailer, _ := p.client.trailerURL(ctx, "tv", result.ID)
	return &Match{
		ProviderID:  result.ID,
		Title:       result.Name,
		Overview:    result.Overview,
		ReleaseDate: result.FirstAirDate,
		PosterURL:   imageURL(result.PosterPath),
		BackdropURL: imageURL(result.BackdropPath),
		TrailerURL:  trailer,
	}, nil
}

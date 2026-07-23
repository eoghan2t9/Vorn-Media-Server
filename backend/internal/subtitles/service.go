package subtitles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Service coordinates moviehash-based subtitle lookup, on-disk caching keyed
// by that hash (so a repeat request for the same file's subtitles never
// touches the OpenSubtitles API, let alone its download quota, again), and
// OpenSubtitles account/session state including the last known download
// quota -- surfaced read-only in the admin UI.
type Service struct {
	client   *Client
	username string
	password string
	cacheDir string

	mu        sync.Mutex
	token     string
	remaining int
	resetTime string
}

func NewService(apiKey, username, password, cacheDir string) (*Service, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("subtitles: creating cache dir: %w", err)
	}
	return &Service{
		client:    NewClient(apiKey),
		username:  username,
		password:  password,
		cacheDir:  cacheDir,
		remaining: -1, // unknown until the first login/download
	}, nil
}

func (svc *Service) cachePath(movieHash, language string) string {
	return filepath.Join(svc.cacheDir, movieHash+"."+language+".vtt")
}

func (svc *Service) ensureToken(ctx context.Context) (string, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.token != "" {
		return svc.token, nil
	}
	login, err := svc.client.Login(ctx, svc.username, svc.password)
	if err != nil {
		return "", err
	}
	svc.token = login.Token
	svc.remaining = login.AllowedDownloads
	return svc.token, nil
}

// Fetch returns the local path to a WebVTT subtitle file for videoPath in
// language, searching OpenSubtitles by videoPath's content hash and
// downloading (then converting from SRT) it only if it isn't already
// cached.
func (svc *Service) Fetch(ctx context.Context, videoPath, language string) (string, error) {
	movieHash, err := ComputeMovieHash(videoPath)
	if err != nil {
		return "", err
	}

	cached := svc.cachePath(movieHash, language)
	if _, err := os.Stat(cached); err == nil {
		return cached, nil
	}

	token, err := svc.ensureToken(ctx)
	if err != nil {
		return "", err
	}

	results, err := svc.client.SearchByMovieHash(ctx, token, movieHash, language)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "", fmt.Errorf("subtitles: no %s subtitles found for this file", language)
	}

	best := results[0]
	for _, r := range results {
		if r.DownloadCount > best.DownloadCount {
			best = r
		}
	}

	dl, err := svc.client.Download(ctx, token, best.FileID)
	if err != nil {
		return "", err
	}

	svc.mu.Lock()
	svc.remaining = dl.Remaining
	svc.resetTime = dl.ResetTime
	svc.mu.Unlock()

	vtt := SRTToVTT(dl.Content)
	if err := os.WriteFile(cached, vtt, 0o644); err != nil {
		return "", fmt.Errorf("subtitles: caching downloaded file: %w", err)
	}
	return cached, nil
}

type Quota struct {
	// Remaining is -1 until the account has actually logged in or
	// downloaded at least once, since OpenSubtitles only reports it then.
	Remaining int
	ResetTime string
}

func (svc *Service) Quota() Quota {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return Quota{Remaining: svc.remaining, ResetTime: svc.resetTime}
}

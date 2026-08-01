// Package config loads Vorn's runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
)

type Config struct {
	HTTPAddr             string
	PostgresDSN          string
	DragonflyAddr        string
	CORSOrigin           string
	DevMode              bool
	TMDbAPIKey           string
	TranscodeOutputDir   string
	TranscodeMaxSessions int
	TorrentEnabled       bool
	TorrentDownloadDir   string
	TorrentPeerPort      int
	NZBEnabled           bool
	OpenSubtitlesAPIKey  string
	OpenSubtitlesUser    string
	OpenSubtitlesPass    string
	SubtitlesCacheDir    string
	ArtworkCacheDir      string
	BackupDir            string
	GitHubRepo             string
	NZBSyncIntervalSeconds           int
	AcquisitionMonitorIntervalSeconds int
	FanartAPIKey                      string
	OMDbAPIKey           string
	TVDbAPIKey           string
	TVDbPin              string
	ProwlarrBaseURL      string
	ProwlarrAPIKey       string
	ProwlarrConfigPath   string
}

func Load() Config {
	return Config{
		HTTPAddr:             getEnv("VORN_HTTP_ADDR", ":8080"),
		PostgresDSN:          getEnv("VORN_POSTGRES_DSN", "postgres://vorn:vorn@localhost:5432/vorn?sslmode=disable"),
		DragonflyAddr:        getEnv("VORN_DRAGONFLY_ADDR", "localhost:6379"),
		CORSOrigin:           getEnv("VORN_CORS_ORIGIN", "http://localhost:5173"),
		DevMode:              getBoolEnv("VORN_DEV_MODE", false),
		TMDbAPIKey:           getEnv("VORN_TMDB_API_KEY", ""),
		TranscodeOutputDir:   getEnv("VORN_TRANSCODE_DIR", os.TempDir()+"/vorn-transcode"),
		TranscodeMaxSessions: getIntEnv("VORN_TRANSCODE_MAX_SESSIONS", runtime.NumCPU()),
		TorrentEnabled:       getBoolEnv("VORN_TORRENT_ENABLED", false),
		TorrentDownloadDir:   getEnv("VORN_TORRENT_DOWNLOAD_DIR", "./data/downloads"),
		TorrentPeerPort:      getIntEnv("VORN_TORRENT_PEER_PORT", 0),
		NZBEnabled:           getBoolEnv("VORN_NZB_ENABLED", false),
		OpenSubtitlesAPIKey:  getEnv("VORN_OPENSUBTITLES_API_KEY", ""),
		OpenSubtitlesUser:    getEnv("VORN_OPENSUBTITLES_USERNAME", ""),
		OpenSubtitlesPass:    getEnv("VORN_OPENSUBTITLES_PASSWORD", ""),
		SubtitlesCacheDir:    getEnv("VORN_SUBTITLES_CACHE_DIR", "./data/subtitles-cache"),
		ArtworkCacheDir:      getEnv("VORN_ARTWORK_CACHE_DIR", "./data/artwork-cache"),
		BackupDir:            getEnv("VORN_BACKUP_DIR", "./data/backups"),
		GitHubRepo:             getEnv("VORN_GITHUB_REPO", "eoghan2t9/Vorn-Media-Server"),
		NZBSyncIntervalSeconds:           getIntEnv("VORN_NZB_SYNC_INTERVAL", 30),
		AcquisitionMonitorIntervalSeconds: getIntEnv("VORN_ACQUISITION_MONITOR_INTERVAL", 1800),
		FanartAPIKey:                      getEnv("VORN_FANART_API_KEY", ""),
		OMDbAPIKey:           getEnv("VORN_OMDB_API_KEY", ""),
		TVDbAPIKey:           getEnv("VORN_TVDB_API_KEY", ""),
		TVDbPin:              getEnv("VORN_TVDB_PIN", ""),
		ProwlarrBaseURL:      getEnv("VORN_PROWLARR_BASE_URL", ""),
		ProwlarrAPIKey:       getEnv("VORN_PROWLARR_API_KEY", ""),
		ProwlarrConfigPath:   getEnv("VORN_PROWLARR_CONFIG_PATH", ""),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getIntEnv(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func (c Config) String() string {
	return fmt.Sprintf("http_addr=%s postgres=<redacted> dragonfly=%s cors_origin=%s dev_mode=%v tmdb_configured=%v transcode_max_sessions=%d torrent_enabled=%v nzb_enabled=%v opensubtitles_configured=%v fanart_configured=%v omdb_configured=%v tvdb_configured=%v prowlarr_configured=%v",
		c.HTTPAddr, c.DragonflyAddr, c.CORSOrigin, c.DevMode, c.TMDbAPIKey != "", c.TranscodeMaxSessions, c.TorrentEnabled, c.NZBEnabled, c.OpenSubtitlesAPIKey != "",
		c.FanartAPIKey != "", c.OMDbAPIKey != "", c.TVDbAPIKey != "", c.ProwlarrBaseURL != "")
}

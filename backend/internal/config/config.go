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
	return fmt.Sprintf("http_addr=%s postgres=<redacted> dragonfly=%s cors_origin=%s dev_mode=%v tmdb_configured=%v transcode_max_sessions=%d",
		c.HTTPAddr, c.DragonflyAddr, c.CORSOrigin, c.DevMode, c.TMDbAPIKey != "", c.TranscodeMaxSessions)
}

// Package config loads Vorn's runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr      string
	PostgresDSN   string
	DragonflyAddr string
	CORSOrigin    string
	DevMode       bool
	TMDbAPIKey    string
}

func Load() Config {
	return Config{
		HTTPAddr:      getEnv("VORN_HTTP_ADDR", ":8080"),
		PostgresDSN:   getEnv("VORN_POSTGRES_DSN", "postgres://vorn:vorn@localhost:5432/vorn?sslmode=disable"),
		DragonflyAddr: getEnv("VORN_DRAGONFLY_ADDR", "localhost:6379"),
		CORSOrigin:    getEnv("VORN_CORS_ORIGIN", "http://localhost:5173"),
		DevMode:       getBoolEnv("VORN_DEV_MODE", false),
		TMDbAPIKey:    getEnv("VORN_TMDB_API_KEY", ""),
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

func (c Config) String() string {
	return fmt.Sprintf("http_addr=%s postgres=<redacted> dragonfly=%s cors_origin=%s dev_mode=%v tmdb_configured=%v",
		c.HTTPAddr, c.DragonflyAddr, c.CORSOrigin, c.DevMode, c.TMDbAPIKey != "")
}

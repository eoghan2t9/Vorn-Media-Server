// Package config loads Vorn's runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	HTTPAddr      string
	PostgresDSN   string
	DragonflyAddr string
	CORSOrigin    string
}

func Load() Config {
	return Config{
		HTTPAddr:      getEnv("VORN_HTTP_ADDR", ":8080"),
		PostgresDSN:   getEnv("VORN_POSTGRES_DSN", "postgres://vorn:vorn@localhost:5432/vorn?sslmode=disable"),
		DragonflyAddr: getEnv("VORN_DRAGONFLY_ADDR", "localhost:6379"),
		CORSOrigin:    getEnv("VORN_CORS_ORIGIN", "http://localhost:5173"),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func (c Config) String() string {
	return fmt.Sprintf("http_addr=%s postgres=<redacted> dragonfly=%s cors_origin=%s", c.HTTPAddr, c.DragonflyAddr, c.CORSOrigin)
}

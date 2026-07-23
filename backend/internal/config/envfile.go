package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadEnvFile reads simple KEY=VALUE lines from path and applies them via
// os.Setenv, skipping blank lines and lines starting with "#". A key
// already set in the real environment is left untouched (the environment
// always wins over the file), so this only fills in gaps.
//
// This exists for platforms without their own "load an env file into this
// service" mechanism -- systemd has EnvironmentFile=, Docker Compose has
// env_file:, but a native Windows service has neither, hence
// install.ps1's `-envfile` flag.
func LoadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("config: opening env file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, alreadySet := os.LookupEnv(key); alreadySet {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("config: setting %s: %w", key, err)
		}
	}
	return scanner.Err()
}

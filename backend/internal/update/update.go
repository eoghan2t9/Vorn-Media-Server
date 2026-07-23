// Package update implements Vorn's self-updater: checking a GitHub
// repository's Releases for a newer version and, on explicit admin request,
// replacing the running executable in place.
//
// This is a no-op (and the HTTP layer returns 503 for it) when running
// under Docker: the container image is the unit of update there, not the
// binary inside it. See IsDockerized.
package update

import (
	"context"
	"fmt"
	"os"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

// IsDockerized reports whether the process is running inside a Docker
// container, via the standard /.dockerenv marker file Docker creates in
// every container's root filesystem.
func IsDockerized() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// CheckResult reports what a check (or apply) found/did.
type CheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	Applied         bool
}

// Service checks repoSlug ("owner/repo") for releases newer than
// currentVersion. It never restarts the process itself -- Vorn may have
// active playback sessions, so that decision (and the restart itself) is
// left to the admin.
type Service struct {
	repoSlug       string
	currentVersion string
}

func NewService(repoSlug, currentVersion string) *Service {
	return &Service{repoSlug: repoSlug, currentVersion: currentVersion}
}

func (s *Service) detectLatest(ctx context.Context) (*selfupdate.Release, bool, error) {
	return selfupdate.DetectLatest(ctx, selfupdate.ParseSlug(s.repoSlug))
}

// Check reports the latest available release without downloading or
// installing anything.
func (s *Service) Check(ctx context.Context) (*CheckResult, error) {
	latest, found, err := s.detectLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("update: checking latest release: %w", err)
	}
	result := &CheckResult{CurrentVersion: s.currentVersion}
	if !found {
		return result, nil
	}
	result.LatestVersion = latest.Version()
	result.UpdateAvailable = !latest.LessOrEqual(s.currentVersion)
	return result, nil
}

// Apply downloads and installs the latest release in place of the running
// executable, if one is newer than currentVersion. The running process
// keeps executing the old binary in memory until it's restarted -- Apply
// does not restart it.
func (s *Service) Apply(ctx context.Context) (*CheckResult, error) {
	latest, found, err := s.detectLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("update: checking latest release: %w", err)
	}
	result := &CheckResult{CurrentVersion: s.currentVersion}
	if !found {
		return result, nil
	}
	result.LatestVersion = latest.Version()
	if latest.LessOrEqual(s.currentVersion) {
		return result, nil
	}
	result.UpdateAvailable = true

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return nil, fmt.Errorf("update: locating running executable: %w", err)
	}
	if err := selfupdate.UpdateTo(ctx, latest.AssetURL, latest.AssetName, exe); err != nil {
		return nil, fmt.Errorf("update: applying update: %w", err)
	}
	result.Applied = true
	return result, nil
}

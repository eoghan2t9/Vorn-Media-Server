// Package version holds Vorn's build version, a single source of truth for
// both the health check endpoint and the self-updater.
package version

// Version is the in-repo source of truth, bumped with every commit (see the
// "Versioning" section of the project conventions): the last number for a
// small change, the middle number for a moderate feature, the first number
// for a big one -- each tier increments independently rather than resetting
// the tiers below it. It can still be overridden at build time via
// -ldflags "-X github.com/eoghan2t9/vorn-media-server/backend/internal/version.Version=v1.2.3"
// (e.g. by the release pipeline that produces bare-metal binaries), but a
// plain `go run`/`go build` (including the Docker image) reports this value
// directly.
var Version = "0.0.3"

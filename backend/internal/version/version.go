// Package version holds Vorn's build version, a single source of truth for
// both the health check endpoint and the self-updater.
package version

// Version is overridden at build time via
// -ldflags "-X github.com/eoghan2t9/vorn-media-server/backend/internal/version.Version=v1.2.3"
// (e.g. by the release pipeline that produces bare-metal binaries); a plain
// `go run`/`go build` without that flag stays "0.0.0-dev".
var Version = "0.0.0-dev"

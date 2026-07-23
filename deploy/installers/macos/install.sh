#!/usr/bin/env bash
set -euo pipefail

# Vorn Media Server bare-metal installer for macOS (launchd).
#
# NOTE: written to macOS/launchd conventions but not tested on real macOS
# hardware (this was built in a Linux-only environment) -- review before
# relying on it, and please report any issues.
#
# What this does:
#   - Installs the vornd binary to /usr/local/bin/vornd
#   - Installs a launchd daemon plist (/Library/LaunchDaemons), templating
#     in the config you're prompted for or pass as env vars, since launchd
#     (unlike systemd) has no separate "environment file" to edit later --
#     to reconfigure, edit the installed plist directly and
#     `launchctl kickstart -k` the service
#   - Loads and starts the service
#
# What this does NOT do -- you need these already, locally or reachable
# over the network:
#   - PostgreSQL (e.g. `brew install postgresql@17`)
#   - A Redis-protocol server (e.g. `brew install dragonflydb/tap/dragonfly`
#     or `brew install redis`)
#   - ffmpeg (e.g. `brew install ffmpeg`; needed for transcoding only,
#     direct-play still works without it)
#
# Usage:
#   ./install.sh /path/to/vornd-binary
#   VORN_VERSION=v1.2.3 ./install.sh   # downloads that release from GitHub
#   ./install.sh                       # downloads the latest release

BINARY_PATH="${1:-}"
INSTALL_DIR="/usr/local/bin"
PLIST_DST="/Library/LaunchDaemons/com.vorn.vornd.plist"
REPO="eoghan2t9/Vorn-Media-Server"

VORN_POSTGRES_DSN="${VORN_POSTGRES_DSN:-postgres://vorn:vorn@localhost:5432/vorn?sslmode=disable}"
VORN_DRAGONFLY_ADDR="${VORN_DRAGONFLY_ADDR:-localhost:6379}"
VORN_CORS_ORIGIN="${VORN_CORS_ORIGIN:-http://localhost:5173}"

if [ "$(id -u)" -ne 0 ]; then
  echo "This installer needs root (it writes to /usr/local/bin and installs a LaunchDaemon)." >&2
  echo "Re-run with sudo." >&2
  exit 1
fi

echo "==> Checking prerequisites (not installed automatically -- see the project README)"
for cmd in ffmpeg ffprobe; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "  warning: $cmd not found on PATH. Transcoding will be unavailable until it's installed; direct-play still works."
  fi
done
echo "  Make sure PostgreSQL and a Redis-protocol server (DragonflyDB or Redis) are reachable --"
echo "  this installer doesn't provision them."
echo

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -z "$BINARY_PATH" ]; then
  VERSION="${VORN_VERSION:-latest}"
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64) GOARCH="amd64" ;;
    arm64) GOARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
  esac

  echo "==> Downloading vornd ($VERSION, darwin/$GOARCH) from GitHub Releases"
  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT

  if [ "$VERSION" = "latest" ]; then
    URL="https://github.com/${REPO}/releases/latest/download/vornd_darwin_${GOARCH}.tar.gz"
  else
    URL="https://github.com/${REPO}/releases/download/${VERSION}/vornd_darwin_${GOARCH}.tar.gz"
  fi

  curl -fsSL "$URL" -o "$TMP_DIR/vornd.tar.gz"
  tar -xzf "$TMP_DIR/vornd.tar.gz" -C "$TMP_DIR"
  BINARY_PATH="$TMP_DIR/vornd"
fi

if [ ! -f "$BINARY_PATH" ]; then
  echo "Binary not found at: $BINARY_PATH" >&2
  exit 1
fi

echo "==> Installing binary to $INSTALL_DIR/vornd"
mkdir -p "$INSTALL_DIR"
install -m 755 "$BINARY_PATH" "$INSTALL_DIR/vornd"

echo "==> Installing LaunchDaemon to $PLIST_DST"
mkdir -p /usr/local/var/log
sed \
  -e "s#<string>postgres://vorn:vorn@localhost:5432/vorn?sslmode=disable</string>#<string>${VORN_POSTGRES_DSN}</string>#" \
  -e "s#<string>localhost:6379</string>#<string>${VORN_DRAGONFLY_ADDR}</string>#" \
  -e "s#<string>http://localhost:5173</string>#<string>${VORN_CORS_ORIGIN}</string>#" \
  "$SCRIPT_DIR/com.vorn.vornd.plist" > "$PLIST_DST"
chown root:wheel "$PLIST_DST"
chmod 644 "$PLIST_DST"

echo "==> Loading service"
launchctl bootout system "$PLIST_DST" 2>/dev/null || true
launchctl bootstrap system "$PLIST_DST"

echo
echo "Installed and started. To reconfigure, edit $PLIST_DST directly, then run:"
echo "  sudo launchctl kickstart -k system/com.vorn.vornd"
echo "Logs: /usr/local/var/log/vornd.log (or use Admin > Logs once it's running)"

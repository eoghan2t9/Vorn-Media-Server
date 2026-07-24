#!/usr/bin/env bash
set -euo pipefail

# Vorn Media Server bare-metal installer for systemd-based Linux.
#
# What this does:
#   - Installs the vornd binary to /usr/local/bin/vornd
#   - Creates a dedicated system user/group "vornd" (owns the binary too,
#     so Admin > Network's self-update can replace it in place)
#   - Writes /etc/vornd/vornd.env if it doesn't already exist (edit this
#     before starting the service)
#   - Installs and enables (but doesn't start) the vornd systemd service
#   - Also installs and enables Prowlarr (indexer manager) as its own
#     system user/service, and points vornd at it via VORN_PROWLARR_BASE_URL/
#     VORN_PROWLARR_CONFIG_PATH -- Vorn then auto-discovers Prowlarr's API
#     key and mirrors whatever indexers you add in Prowlarr's own UI into
#     Vorn's Torrent/NZB Indexers, same as the Docker Compose "prowlarr"
#     profile. Set VORN_INSTALL_PROWLARR=false to skip this (e.g. you
#     already run your own Prowlarr elsewhere).
#
# What this does NOT do -- you need these already, locally or reachable
# over the network; this installer only warns if they're missing, it
# doesn't install system packages across every distro's package manager:
#   - PostgreSQL
#   - A Redis-protocol server (DragonflyDB recommended, but real Redis works)
#   - ffmpeg/ffprobe (needed for transcoding; direct-play still works without it)
#
# Usage:
#   sudo ./install.sh /path/to/vornd-binary
#   sudo VORN_VERSION=v1.2.3 ./install.sh   # downloads that release from GitHub
#   sudo ./install.sh                       # downloads the latest release
#   sudo VORN_INSTALL_PROWLARR=false ./install.sh   # skip bundling Prowlarr

BINARY_PATH="${1:-}"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/vornd"
SERVICE_USER="vornd"
REPO="eoghan2t9/Vorn-Media-Server"
INSTALL_PROWLARR="${VORN_INSTALL_PROWLARR:-true}"
PROWLARR_INSTALL_DIR="/opt/prowlarr"
PROWLARR_DATA_DIR="/var/lib/prowlarr"
PROWLARR_USER="prowlarr"

if [ "$(id -u)" -ne 0 ]; then
  echo "This installer needs root (it creates a system user, writes to /usr/local/bin and /etc, and installs a systemd unit)." >&2
  echo "Re-run with sudo." >&2
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemctl not found -- this installer only supports systemd-based Linux distributions." >&2
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
    aarch64|arm64) GOARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
  esac

  echo "==> Downloading vornd ($VERSION, linux/$GOARCH) from GitHub Releases"
  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT

  if [ "$VERSION" = "latest" ]; then
    URL="https://github.com/${REPO}/releases/latest/download/vornd_linux_${GOARCH}.tar.gz"
  else
    URL="https://github.com/${REPO}/releases/download/${VERSION}/vornd_linux_${GOARCH}.tar.gz"
  fi

  curl -fsSL "$URL" -o "$TMP_DIR/vornd.tar.gz"
  tar -xzf "$TMP_DIR/vornd.tar.gz" -C "$TMP_DIR"
  BINARY_PATH="$TMP_DIR/vornd"
fi

if [ ! -f "$BINARY_PATH" ]; then
  echo "Binary not found at: $BINARY_PATH" >&2
  exit 1
fi

if ! id "$SERVICE_USER" >/dev/null 2>&1; then
  echo "==> Creating system user '$SERVICE_USER'"
  useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi

echo "==> Installing binary to $INSTALL_DIR/vornd"
install -m 755 -o "$SERVICE_USER" -g "$SERVICE_USER" "$BINARY_PATH" "$INSTALL_DIR/vornd"

mkdir -p "$CONFIG_DIR" /var/lib/vornd
chown "$SERVICE_USER:$SERVICE_USER" /var/lib/vornd

if [ ! -f "$CONFIG_DIR/vornd.env" ]; then
  echo "==> Writing default config to $CONFIG_DIR/vornd.env (edit this before starting the service)"
  cat > "$CONFIG_DIR/vornd.env" <<'EOF'
# Vorn Media Server configuration. See the project README for the full list
# of environment variables (torrent/NZB/debrid, OpenSubtitles, SSL, etc).
VORN_HTTP_ADDR=:8080
VORN_POSTGRES_DSN=postgres://vorn:vorn@localhost:5432/vorn?sslmode=disable
VORN_DRAGONFLY_ADDR=localhost:6379
VORN_CORS_ORIGIN=http://localhost:5173
EOF
  chmod 600 "$CONFIG_DIR/vornd.env"
  chown "$SERVICE_USER:$SERVICE_USER" "$CONFIG_DIR/vornd.env"
else
  echo "==> $CONFIG_DIR/vornd.env already exists, leaving it as-is"
fi

echo "==> Installing systemd service"
cp "$SCRIPT_DIR/vornd.service" /etc/systemd/system/vornd.service
systemctl daemon-reload
systemctl enable vornd

if [ "$INSTALL_PROWLARR" = "true" ]; then
  echo "==> Installing Prowlarr (indexer manager) -- set VORN_INSTALL_PROWLARR=false to skip this"
  PROWLARR_ARCH_RAW="$(uname -m)"
  case "$PROWLARR_ARCH_RAW" in
    x86_64) PROWLARR_ARCH="x64" ;;
    aarch64|arm64) PROWLARR_ARCH="arm64" ;;
    *) echo "  warning: unsupported architecture for Prowlarr ($PROWLARR_ARCH_RAW), skipping"; INSTALL_PROWLARR="false" ;;
  esac

  # This whole block degrades to a warning rather than aborting the script
  # on any failure (network hiccup reaching GitHub, unexpected release
  # asset layout, etc.) -- vornd itself is already fully installed by this
  # point, and a Prowlarr provisioning problem shouldn't take that down
  # with it. PROWLARR_URL empty is the single signal used to skip the rest.
  if [ "$INSTALL_PROWLARR" = "true" ]; then
    PROWLARR_ASSET_PATTERN="linux-core-${PROWLARR_ARCH}.tar.gz"
    PROWLARR_URL=""
    if PROWLARR_RELEASE_JSON="$(curl -fsSL https://api.github.com/repos/Prowlarr/Prowlarr/releases/latest)"; then
      PROWLARR_URL="$(printf '%s' "$PROWLARR_RELEASE_JSON" | { grep -o "\"browser_download_url\"[[:space:]]*:[[:space:]]*\"[^\"]*${PROWLARR_ASSET_PATTERN}\"" || true; } | head -1 | sed -E 's/.*"(https:[^"]+)"/\1/')"
    fi

    if [ -z "$PROWLARR_URL" ]; then
      echo "  warning: could not resolve a Prowlarr release asset for linux-core-${PROWLARR_ARCH} -- skipping Prowlarr install" >&2
      echo "  you can install it yourself later from https://github.com/Prowlarr/Prowlarr/releases and set" >&2
      echo "  VORN_PROWLARR_BASE_URL/VORN_PROWLARR_CONFIG_PATH in $CONFIG_DIR/vornd.env" >&2
    else
      # Not registered via `trap ... EXIT` -- that would clobber the trap
      # already set (if any) for vornd's own download tmpdir above, since
      # bash keeps only the most recently registered handler per signal.
      # Removed explicitly at the end of this block instead.
      PROWLARR_TMP_DIR="$(mktemp -d)"
      curl -fsSL "$PROWLARR_URL" -o "$PROWLARR_TMP_DIR/prowlarr.tar.gz"
      tar -xzf "$PROWLARR_TMP_DIR/prowlarr.tar.gz" -C "$PROWLARR_TMP_DIR"

      if ! id "$PROWLARR_USER" >/dev/null 2>&1; then
        useradd --system --no-create-home --shell /usr/sbin/nologin "$PROWLARR_USER"
      fi

      rm -rf "$PROWLARR_INSTALL_DIR"
      mv "$PROWLARR_TMP_DIR/Prowlarr" "$PROWLARR_INSTALL_DIR"
      chown -R "$PROWLARR_USER:$PROWLARR_USER" "$PROWLARR_INSTALL_DIR"
      mkdir -p "$PROWLARR_DATA_DIR"
      chown "$PROWLARR_USER:$PROWLARR_USER" "$PROWLARR_DATA_DIR"

      cp "$SCRIPT_DIR/prowlarr.service" /etc/systemd/system/prowlarr.service
      systemctl daemon-reload
      systemctl enable prowlarr

      if ! grep -q '^VORN_PROWLARR_BASE_URL=' "$CONFIG_DIR/vornd.env" 2>/dev/null; then
        {
          echo "VORN_PROWLARR_BASE_URL=http://localhost:9696"
          echo "VORN_PROWLARR_CONFIG_PATH=${PROWLARR_DATA_DIR}/config.xml"
        } >> "$CONFIG_DIR/vornd.env"
      fi

      rm -rf "$PROWLARR_TMP_DIR"
    fi
  fi
fi

echo
echo "Installed. Edit $CONFIG_DIR/vornd.env, then start with:"
echo "  systemctl start vornd"
if [ "$INSTALL_PROWLARR" = "true" ]; then
  echo "  systemctl start prowlarr"
fi
echo "  journalctl -u vornd -f    # or use Admin > Logs once it's running"

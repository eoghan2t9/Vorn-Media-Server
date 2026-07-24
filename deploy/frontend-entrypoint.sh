#!/bin/sh
# The production build bakes VITE_VORN_API_BASE in at build time (that's how
# Vite env vars work -- import.meta.env.* is inlined into the bundle, it
# isn't read from the environment at request time the way the old dev-server
# image could get away with). To keep this one image usable across different
# deployments (different VORN_API_BASE per install) without rebuilding it
# per-deployment, the build stage bakes in a placeholder token instead of a
# real value, and this entrypoint substitutes it for the *actual* runtime
# VITE_VORN_API_BASE right before starting the server.
set -e

PLACEHOLDER="__VORN_RUNTIME_API_BASE__"
API_BASE="${VITE_VORN_API_BASE:-http://localhost:8080}"

grep -rl "$PLACEHOLDER" /app/dist/assets 2>/dev/null | while IFS= read -r f; do
  sed -i "s|$PLACEHOLDER|$API_BASE|g" "$f"
done

exec "$@"

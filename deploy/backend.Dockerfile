# syntax=docker/dockerfile:1
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /out/vornd ./cmd/vornd

FROM debian:bookworm-slim
# postgresql-client-17 provides pg_dump/psql for the admin backup/restore
# feature, matching the postgres:17-alpine image docker-compose.yml runs.
# This isn't just a nice-to-have version match: pg_dump actively refuses
# to run at all against a server newer than itself ("aborting because of
# server version mismatch") rather than merely warning, and bookworm's
# default `postgresql-client` package is only v15 -- so the exact version
# has to come from the official PGDG apt repo, added here the standard way
# (fetch their signing key, add the repo, install, then remove the
# now-unneeded fetch tools).
RUN apt-get update && apt-get install -y --no-install-recommends \
      ffmpeg ca-certificates curl gnupg \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor -o /usr/share/keyrings/postgresql.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/postgresql.gpg] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" > /etc/apt/sources.list.d/pgdg.list \
    && apt-get update && apt-get install -y --no-install-recommends postgresql-client-17 \
    && apt-get purge -y --auto-remove curl gnupg \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/vornd /usr/local/bin/vornd
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/vornd"]

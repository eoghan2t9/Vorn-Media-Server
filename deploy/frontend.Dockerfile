# syntax=docker/dockerfile:1
# Production build: a real `vite build`, served statically by `vite preview`
# -- not the dev server. Running `vite dev` in production (the old approach)
# meant every client had an active HMR websocket to the dev server; every
# time this container restarted (e.g. a routine redeploy), connected
# browsers would get hit with Vite's own reconnect-triggered full-page
# reload, which is what looked like the frontend "randomly refreshing
# itself". A production build has no HMR client at all, so there's nothing
# left to trigger that.
FROM node:22-bookworm-slim AS build
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm install
COPY frontend/ ./
# Baked in as a placeholder, not a real value -- see frontend-entrypoint.sh.
# Vite inlines import.meta.env.VITE_* at build time, so the real,
# deployment-specific API base can't be a build-time value here without
# rebuilding this image per-deployment.
ENV VITE_VORN_API_BASE=__VORN_RUNTIME_API_BASE__
RUN npm run build

FROM node:22-bookworm-slim
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm install
COPY --from=build /app/dist ./dist
COPY deploy/frontend-entrypoint.sh /usr/local/bin/frontend-entrypoint.sh
RUN chmod +x /usr/local/bin/frontend-entrypoint.sh
EXPOSE 5173
ENTRYPOINT ["/usr/local/bin/frontend-entrypoint.sh"]
CMD ["npx", "vite", "preview", "--host", "0.0.0.0", "--port", "5173"]

# syntax=docker/dockerfile:1
# Development image: runs the Vite dev server with HMR.
# A production build (static assets served by the backend or nginx) lands in a later phase.
FROM node:22-bookworm-slim
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm install
COPY frontend/ ./
EXPOSE 5173
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]

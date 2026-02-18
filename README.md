# healthmon

[![CI](https://github.com/nikaarh/healthmon/actions/workflows/ci.yml/badge.svg)](https://github.com/nikaarh/healthmon/actions/workflows/ci.yml)
[![Release](https://github.com/nikaarh/healthmon/actions/workflows/release.yml/badge.svg)](https://github.com/nikaarh/healthmon/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE-MIT)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE-APACHE)

Healthmon watches Docker containers via the Docker socket, detects restart loops, healing, and container recreation/image changes, and exposes a real-time dashboard with REST + WebSocket updates. Alerts are pushed to Telegram.

## Features

- Detect restart loops (red), healed restart loops (green), image change or other recreate events (blue).
- Keeps full event history and container metadata in SQLite.
- REST API + WebSocket updates for live UI.
- Single static binary and scratch Docker image.

## Configuration

| Env var | Default | Description |
| --- | --- | --- |
| `HM_DB_PATH` | `./healthmon.db` | SQLite DB path |
| `HM_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker host URL (e.g. `unix:///var/run/docker.sock` or `tcp://socket-proxy:2375`) |
| `HM_HTTP_ADDR` | `:8080` | HTTP bind address |
| `HM_TG_ENABLED` | `false` | Enable Telegram alerts |
| `HM_TG_TOKEN` | (empty) | Telegram bot token (required if enabled) |
| `HM_TG_CHAT_ID` | (empty) | Telegram chat ID (required if enabled) |
| `HM_RESTART_WINDOW_SECONDS` | `300` | Restart loop window |
| `HM_RESTART_THRESHOLD` | `3` | Restart loop threshold |
| `HM_EVENT_CACHE_LIMIT` | `5000` | Event cache limit |

## Run with Docker

Recommended: use a Docker socket proxy like https://github.com/11notes/docker-socket-proxy instead of mounting the raw socket.

```bash
docker run --rm \
  -p 8080:8080 \
  -e HM_DB_PATH=/data/healthmon.db \
  -e HM_TG_ENABLED=true \
  -e HM_TG_TOKEN=YOUR_TOKEN \
  -e HM_TG_CHAT_ID=YOUR_CHAT_ID \
  -e HM_DOCKER_HOST=tcp://socket-proxy:2375 \
  -v $(pwd)/data:/data \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  ghcr.io/nikaarh/healthmon:latest
```

## Run with docker-compose

```yaml
services:
  socket-proxy:
    image: ghcr.io/11notes/docker-socket-proxy:stable
    restart: unless-stopped
    environment:
      CONTAINERS: 1
      EVENTS: 1
      INFO: 1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  healthmon:
    image: ghcr.io/nikaarh/healthmon:latest
    ports:
      - "8080:8080"
    environment:
      HM_DB_PATH: /data/healthmon.db
      HM_TG_ENABLED: true
      HM_TG_TOKEN: YOUR_TOKEN
      HM_TG_CHAT_ID: YOUR_CHAT_ID
      HM_DOCKER_HOST: tcp://socket-proxy:2375
    user: "65534:65534"
    volumes:
      - ./data:/data
    read_only: true
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    depends_on:
      - socket-proxy
```

## Local development

Backend (Go):

```bash
go run ./cmd/healthmon
```

Frontend (Vite + hot reload):

```bash
cd web
npm install
npm run dev
```

Then open `http://localhost:5173` for the UI (Vite dev server) while the backend runs on `http://localhost:8080`.

## REST API

- `GET /api/containers` returns all containers with current status and last event.
- `GET /api/containers/{name}/events?before_id={id}&limit={n}` returns paginated events.
- `GET /api/events/stream` WebSocket pushes live updates.

## License

Licensed under either MIT (`LICENSE-MIT`) or Apache-2.0 (`LICENSE-APACHE`).

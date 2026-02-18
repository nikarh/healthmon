# healthmon

[![CI](https://github.com/OWNER/REPO/actions/workflows/ci.yml/badge.svg)](https://github.com/OWNER/REPO/actions/workflows/ci.yml)
[![Release](https://github.com/OWNER/REPO/actions/workflows/release.yml/badge.svg)](https://github.com/OWNER/REPO/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE-MIT)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE-APACHE)

Healthmon watches Docker containers via the Docker socket, detects restart loops, healing, and container recreation/image changes, and exposes a real-time dashboard with REST + WebSocket updates. Alerts are pushed to Telegram.

Replace `OWNER/REPO` in image and badge URLs with your GitHub org/user and repo name.

## Features

- Detect restart loops (red), healed restart loops (green), image change or other recreate events (blue).
- Keeps full event history and container metadata in SQLite.
- REST API + WebSocket updates for live UI.
- Single static binary and scratch Docker image.

## Configuration

| Env var | Default | Description |
| --- | --- | --- |
| `HM_DB_PATH` | `./healthmon.db` | SQLite DB path |
| `HM_DOCKER_SOCKET` | `/var/run/docker.sock` | Docker socket path |
| `HM_HTTP_ADDR` | `:8080` | HTTP bind address |
| `HM_TG_TOKEN` | (empty) | Telegram bot token |
| `HM_TG_CHAT_ID` | (empty) | Telegram chat ID |
| `HM_RESTART_WINDOW_SECONDS` | `300` | Restart loop window |
| `HM_RESTART_THRESHOLD` | `3` | Restart loop threshold |
| `HM_EVENT_CACHE_LIMIT` | `5000` | Event cache limit |

## Run with Docker

```bash
docker run --rm \
  -p 8080:8080 \
  -e HM_DB_PATH=/data/healthmon.db \
  -e HM_TG_TOKEN=YOUR_TOKEN \
  -e HM_TG_CHAT_ID=YOUR_CHAT_ID \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v $(pwd)/data:/data \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  ghcr.io/OWNER/REPO/healthmon:latest
```

## Run with docker-compose

```yaml
services:
  healthmon:
    image: ghcr.io/OWNER/REPO/healthmon:latest
    ports:
      - "8080:8080"
    environment:
      HM_DB_PATH: /data/healthmon.db
      HM_TG_TOKEN: YOUR_TOKEN
      HM_TG_CHAT_ID: YOUR_CHAT_ID
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./data:/data
    read_only: true
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
```

## REST API

- `GET /api/containers` returns all containers with current status and last event.
- `GET /api/containers/{name}/events?before_id={id}&limit={n}` returns paginated events.
- `GET /api/events/stream` WebSocket pushes live updates.

## License

Licensed under either MIT (`LICENSE-MIT`) or Apache-2.0 (`LICENSE-APACHE`).

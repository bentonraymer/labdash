# LabDash

A single, self-hosted dashboard for a homelab: a glassy launchpad with Spotlight search, uptime monitoring with Pushover alerts, network scanning, IPAM/port tracking, a service inventory, and markdown docs. Everything except the initial secret is configured in the GUI.

## Run it

```bash
openssl rand -hex 32   # put this in .env as LABDASH_SECRET=...
docker compose up -d
```

Then open `http://<host>:8080` and create the admin account.

`docker-compose.yml` uses `network_mode: host` and `NET_RAW`/`NET_ADMIN` so the scanner can read MAC addresses, ping, and send Wake-on-LAN packets. Data (SQLite DB, uploads, cached icons, wallpapers) lives in `./data`.

| Env var | Default | Purpose |
|---|---|---|
| `LABDASH_SECRET` | generated into `/data/secret.key` | Encrypts stored API credentials |
| `PORT` | `8080` | HTTP port |
| `LABDASH_DATA_DIR` | `/data` | Data directory |
| `LABDASH_DEBUG` | — | `1` for debug logs |

## Develop

- Backend: Go 1.26 (`cmd/labdash`, `internal/*`, SQL migrations in `migrations/`).
- Frontend: React + Vite + Tailwind in `web/`.

```bash
# backend (needs Go, or use the golang:1.26-alpine container)
LABDASH_DATA_DIR=./data go run ./cmd/labdash
```

```bash
# frontend with hot reload, proxying /api to :8080
cd web && npm install && npm run dev
```

```bash
# tests
go test ./internal/... && (cd web && npm test)
```

Releases: pushing a `v*` tag builds a multi-arch image (`linux/amd64`, `linux/arm64`) to `ghcr.io/bentonraymer/labdash`. Pushes to `main` publish an `edge` tag.

## Keyboard

- `⌘K` / `Ctrl+K` / `/`: Spotlight.
- `↵` opens the top result; `⇧↵` opens it in a new tab; `Tab` completes the name.
- Right-click a tile to edit it. Use the pencil in the header to drag tiles between sections.

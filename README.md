# LabDash

A single, self-hosted dashboard for a homelab: a glassy launchpad with Spotlight search, uptime monitoring with Pushover alerts, network scanning, IPAM/port tracking, a service inventory, and markdown docs. Everything except the initial secret is configured in the GUI.

## Run it

```bash
cp .env.example .env
openssl rand -hex 32   # paste into .env as LABDASH_SECRET=...
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

## Integrations (all optional, configured in Settings)

| Integration | What LabDash does | Credentials |
|---|---|---|
| **Pushover** | Down/up alerts, new-device alerts | User key + app token |
| **Nginx Proxy Manager** | Shows whether each service's DNS name has a proxy host (and that it forwards to the right IP:port); creates missing ones from the service editor or Add Service wizard. Never edits or deletes existing hosts. | NPM admin email + password |
| **Proxmox VE** | Read-only CPU/RAM/disk for nodes and for VMs/LXCs you link by VMID | API token with the `PVEAuditor` role |
| **Docker hosts** | Update checks: reads each container's image and compares it with its registry (newer image for the same tag, or a newer version tag). Read-only. | None: run a [socket proxy](https://docs.linuxserver.io/images/docker-socket-proxy/) with `CONTAINERS=1 IMAGES=1 POST=0` on each host (Settings → Docker hosts has a compose file) |
| **GitHub** | Latest releases and release notes for the Updates page, including Proxmox Helper Script apps | Optional token (no scopes) to raise the 60 lookups/hour limit |

Pending Proxmox node updates on the Updates page need the API token to have `Sys.Modify` on `/nodes` (Proxmox's own requirement for listing updates). Without it, the page shows a note instead.

Credentials are encrypted with `LABDASH_SECRET`.

## Backups

Settings → Backup & restore downloads a zip of the database plus uploads, custom icons and wallpapers. A nightly backup is written to `./data/backups` (03:30, keeps the last 7 by default). Restoring stages the archive and restarts the container to apply it, so keep `restart: unless-stopped` in your compose file. The replaced database is kept as `backups/pre-restore-*.db`.

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

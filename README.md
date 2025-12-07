# Docksmith

A Docker container update manager for self-hosters. Monitors your compose stacks, checks registries for newer versions, and provides a clean web UI for managing updates with rollback support.

## Features

- **Automatic Discovery** — Scans mounted directories for Docker Compose files
- **Update Detection** — Checks Docker Hub, GHCR, and private registries
- **Version Constraints** — Pin major/minor versions, set min/max, or use regex filters
- **Pre-Update Checks** — Run custom scripts before updates (e.g., block if Plex has active streams)
- **Rollback Support** — Automatic backups before updates with one-click restore
- **Compose Preservation** — Updates images without touching your comments or formatting
- **Health Monitoring** — Waits for containers to become healthy after updates

## Documentation

| Guide | Description |
|-------|-------------|
| [Labels](docs/labels.md) | All container labels and common patterns |
| [Version Constraints](docs/version-constraints.md) | Pin versions, set bounds, regex filtering |
| [Pre-Update Scripts](docs/scripts.md) | Block updates with custom checks |
| [Rollback](docs/rollback.md) | Backup and recovery procedures |
| [Registries](docs/registries.md) | Docker Hub, GHCR, private registry setup |
| [Integrations](docs/integrations.md) | Homepage, Traefik, Caddy, Tailscale |
| [API Reference](docs/api.md) | REST API endpoints |
| [CLI Reference](docs/cli.md) | Command-line usage |

## Quick Start

```yaml
# docker-compose.yml
services:
  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    container_name: docksmith
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - /path/to/your/stacks:/stacks:rw
    environment:
      - CHECK_INTERVAL=1h
    command: ["api", "--port", "8080"]
```

```bash
docker compose up -d
```

Open http://localhost:8080

## Security

**Docksmith has no built-in authentication.** Deploy it behind:
- A VPN (Tailscale, WireGuard)
- An authenticating reverse proxy (Authelia, Authentik)
- A firewall restricting access to trusted IPs

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CHECK_INTERVAL` | `1h` | How often to check for updates (`5m`, `1h`, `24h`) |
| `CACHE_TTL` | `1h` | How long to cache registry responses |
| `DB_PATH` | `/data/docksmith.db` | SQLite database location |
| `GITHUB_TOKEN` | — | Token for private GHCR images |

### Volume Mounts

| Path | Purpose |
|------|---------|
| `/var/run/docker.sock` | Docker daemon access (required) |
| `/data` | Database persistence (required) |
| `~/.docker/config.json` | Registry auth for private images (optional, read-only) |
| Your stack directories | Compose files to monitor (read-write) |

## Container Labels

Add these to your container definitions to control Docksmith behavior:

### Basic

```yaml
labels:
  - docksmith.ignore=true              # Skip this container
  - docksmith.allow-latest=true        # Allow :latest without warnings
  - docksmith.pre-update-check=/scripts/check.sh  # Run before updates
  - docksmith.restart-after=container  # Restart after another updates
```

### Version Constraints

```yaml
labels:
  # Stay on PostgreSQL 16.x (won't upgrade to 17.x)
  - docksmith.version-pin-major=true

  # Stay on 16.1.x (won't upgrade to 16.2.x)
  - docksmith.version-pin-minor=true

  # Only consider tags matching pattern
  - docksmith.tag-regex=^v?[0-9]+\.[0-9]+$

  # Version bounds
  - docksmith.version-min=2.0.0
  - docksmith.version-max=3.0.0
```

### Common Patterns

**Database — Pin to major version:**
```yaml
postgres:
  image: postgres:16
  labels:
    - docksmith.version-pin-major=true
```

**Media server — Check for active users:**
```yaml
plex:
  image: ghcr.io/linuxserver/plex:latest
  labels:
    - docksmith.allow-latest=true
    - docksmith.pre-update-check=/scripts/check-plex.sh
```

**Reverse proxy — Update nginx after apps:**
```yaml
nginx:
  image: nginx:alpine
  labels:
    - docksmith.restart-after=myapp
```

## Pre-Update Scripts

Scripts run before updates. Exit 0 to allow, non-zero to block.

```bash
#!/bin/bash
# /scripts/check-plex.sh — Block updates if Plex has active streams

PLEX_URL="http://plex:32400"
PLEX_TOKEN="your-token"

SESSIONS=$(curl -s "${PLEX_URL}/status/sessions?X-Plex-Token=${PLEX_TOKEN}" | \
  grep -c "MediaContainer size")

if [ "$SESSIONS" -gt 0 ]; then
  echo "Plex has active streams, skipping update"
  exit 1
fi

exit 0
```

Mount the scripts directory:
```yaml
volumes:
  - ./scripts:/scripts:ro
```

See [docs/scripts.md](docs/scripts.md) for more examples.

## API Reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | Health check |
| GET | `/api/check` | Check all containers for updates |
| GET | `/api/status` | Last check time and status |
| POST | `/api/update` | Update a container |
| POST | `/api/update/batch` | Batch update containers |
| POST | `/api/rollback` | Rollback to previous version |
| GET | `/api/operations` | Update history |
| GET | `/api/backups` | Available rollback points |
| POST | `/api/restart/container/{name}` | Restart container |
| GET | `/api/labels/{container}` | Get container labels |
| POST | `/api/labels/set` | Set labels (restarts container) |
| GET | `/api/events` | SSE stream for real-time progress |

See [docs/api.md](docs/api.md) for full endpoint documentation.

## CLI

```bash
# Check for updates
docksmith check
docksmith check --stack media
docksmith check --filter nginx

# Update containers
docksmith update --container nginx
docksmith update --all

# Rollback
docksmith rollback <operation-id>

# History
docksmith history
docksmith operations

# Server
docksmith api --port 8080
```

See [docs/cli.md](docs/cli.md) for all commands and flags.

## Troubleshooting

### Permission Denied on Docker Socket

Docksmith runs as UID 1000 with GID 972. If your docker group has a different GID:

```bash
# Check your docker group
getent group docker
```

### Registry Rate Limits

For Docker Hub authenticated access:
```yaml
volumes:
  - ~/.docker/config.json:/home/docksmith/.docker/config.json:ro
```

For private GHCR images:
```yaml
environment:
  - GITHUB_TOKEN=ghp_xxxxxxxxxxxx
```

### Container Not Discovered

1. Ensure the compose directory is mounted with `:rw`
2. Container must be managed by Docker Compose (has `com.docker.compose.project` label)
3. Check it's not labeled `docksmith.ignore=true`

## Reverse Proxy Examples

### Traefik

```yaml
services:
  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - /home/user/stacks:/stacks:rw
    command: ["api", "--port", "8080"]
    labels:
      - traefik.enable=true
      - traefik.http.routers.docksmith.rule=Host(`docksmith.example.com`)
      - traefik.http.routers.docksmith.entrypoints=websecure
      - traefik.http.routers.docksmith.tls.certresolver=letsencrypt
      - traefik.http.services.docksmith.loadbalancer.server.port=8080
      - docksmith.ignore=true  # Don't let Docksmith update itself
    networks:
      - traefik
```

### Caddy

```
docksmith.example.com {
    reverse_proxy docksmith:8080
}
```

See [docs/integrations.md](docs/integrations.md) for more reverse proxy and dashboard integrations.

## License

MIT

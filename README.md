<p align="center">
  <img src="ui/public/docksmith-title.svg" alt="Docksmith" width="400">
</p>

<p align="center">
  A Docker container update manager for self-hosters.<br>
  Monitors your compose stacks, checks registries for newer versions, and provides a web UI for managing updates.
</p>

## Features

- **Automatic Discovery** — Finds containers from mounted compose directories
- **Update Detection** — Checks Docker Hub, GHCR, and private registries
- **Version Constraints** — Pin major/minor versions, regex filters, min/max bounds
- **Pre-Update Checks** — Run scripts before updates (e.g., block if Plex has streams)
- **Rollback Support** — Revert to previous versions when updates cause issues
- **Dependency Awareness** — Restart dependent containers (VPN tunnels, sidecars)

## Quick Start

```yaml
services:
  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    container_name: docksmith
    restart: unless-stopped
    ports:
      - "3000:3000"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - /home/user/stacks:/stacks:rw  # Your compose directories
    # Starts on port 3000 by default, use --port to change
```

Open http://localhost:3000

## Stack Discovery

Docksmith reads container labels to find compose file paths. Mount the host directories where your compose files live:

```yaml
volumes:
  - /home/user/stacks:/stacks:rw
```

The container mount path can be anything—Docksmith automatically translates between host and container paths.

**Multiple locations?** Mount each:

```yaml
volumes:
  - /home/user/stacks:/stacks:rw
  - /opt/docker:/docker:rw
```

**Using `env_file` in your compose files?** Add mirror mounts so files are accessible at their original host paths:

```yaml
volumes:
  - /home/user/stacks:/stacks:rw
  - /home/user/stacks:/home/user/stacks:rw  # Mirror mount for env_file
```

This is required because `docker compose` resolves `env_file: .env` relative to the host path during recreation.

**Reserved paths** (don't mount over these):
- `/app` — Docksmith binary and UI
- `/data` — Default database location (change with `DB_PATH` env var if needed)

**Requirements:**
- Mount with `:rw` — Docksmith edits compose files to update image tags
- Containers must be managed by Docker Compose
- Compose `include` is supported

## Security

**Docksmith has no built-in authentication.** Deploy behind a VPN, authenticating proxy, or firewall.

## Documentation

| Guide | Description |
|-------|-------------|
| [Labels](docs/labels.md) | Container labels and version constraints |
| [Scripts](docs/scripts.md) | Pre-update check scripts |
| [Registries](docs/registries.md) | Docker Hub, GHCR, private registries |
| [Integrations](docs/integrations.md) | Homepage, Traefik, Tailscale |
| [API](docs/api.md) | REST API reference |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `CHECK_INTERVAL` | `5m` | How often to check for updates |
| `CACHE_TTL` | `1h` | Registry response cache duration |
| `DB_PATH` | `/data/docksmith.db` | Database location |

For private registries, mount your docker config:

```yaml
volumes:
  - ~/.docker/config.json:/home/docksmith/.docker/config.json:ro
```

## License

MIT
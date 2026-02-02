<p align="center">
  <img src="ui/public/docksmith-title.svg" alt="Docksmith" width="420">
</p>

<p align="center">
  <strong>A smarter way to manage Docker container updates.</strong><br>
  Monitor your compose stacks, check for newer versions, update with confidence, and rollback when needed.
</p>

<p align="center">
  <a href="https://github.com/chrisae9/docksmith/blob/main/LICENSE"><img src="https://img.shields.io/github/license/chrisae9/docksmith" alt="License"></a>
  <a href="https://github.com/chrisae9/docksmith/pkgs/container/docksmith"><img src="https://img.shields.io/badge/ghcr.io-docksmith-blue" alt="GHCR"></a>
</p>

---

## Why Docksmith?

Unlike Watchtower which updates containers automatically and silently, Docksmith gives you **visibility and control**:

| | Docksmith | Watchtower |
|---|:---:|:---:|
| Web UI | ✅ | ❌ |
| See available updates before applying | ✅ | ❌ |
| Rollback to previous version | ✅ | ❌ |
| Pre-update checks (e.g., block if Plex is streaming) | ✅ | ❌ |
| Version constraints (pin major/minor) | ✅ | ❌ |
| Automatic updates | ❌ | ✅ |

**Docksmith is for self-hosters who want to know what's updating and when.**

---

## Quick Start

```bash
docker run -d \
  --name docksmith \
  -p 8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v ./data:/data \
  -v /path/to/your/stacks:/stacks:rw \
  ghcr.io/chrisae9/docksmith:latest
```

Open **http://localhost:8080** and you're done.

<details>
<summary><strong>Docker Compose (recommended)</strong></summary>

```yaml
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
```

```bash
docker compose up -d
```

</details>

> **Note**: Mount your compose directories with `:rw` — Docksmith edits compose files to update image tags.

---

## Features

- **Update Dashboard** — See all containers with available updates at a glance
- **One-Click Updates** — Update individual containers or batch update multiple
- **Rollback Support** — Revert to previous versions when updates cause issues
- **Pre-Update Checks** — Run scripts before updates (block if Plex has active streams, backup databases first, etc.)
- **Version Constraints** — Pin to major/minor versions, use regex filters, set min/max bounds
- **Explorer** — Browse and manage containers, images, networks, and volumes
- **Container Controls** — Stop, start, restart, and remove containers directly from the UI
- **Prune** — Clean up unused images, containers, networks, and volumes

---

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `CHECK_INTERVAL` | `5m` | How often to check for updates |
| `CACHE_TTL` | `1h` | Registry response cache duration |
| `DB_PATH` | `/data/docksmith.db` | Database location |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `GITHUB_TOKEN` | - | For private GHCR images |

For private registries, mount your Docker config:

```yaml
volumes:
  - ~/.docker/config.json:/home/docksmith/.docker/config.json:ro
```

---

## Documentation

| Guide | Description |
|-------|-------------|
| [Labels](docs/labels.md) | Container labels for version constraints, pre-update checks |
| [Scripts](docs/scripts.md) | Pre-update check script examples |
| [Registries](docs/registries.md) | Docker Hub, GHCR, private registry setup |
| [Integrations](docs/integrations.md) | Homepage widget, Tailscale, Traefik |
| [API](docs/api.md) | REST API reference |

---

## Security

**Docksmith has no built-in authentication.** It should only be accessed on trusted networks.

See [Integrations](docs/integrations.md) for secure deployment with Tailscale.

---

## About

Docksmith was built as an AI-first project — developed collaboratively with Claude as a coding partner. While AI assisted throughout development, significant effort went into architecture decisions, testing, and refinement to create a polished, production-ready tool for the self-hosting community.

---

## License

MIT

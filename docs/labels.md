# Container Labels

Configure Docksmith behavior using Docker labels in your compose files.

## Contents

- [Quick Reference](#quick-reference)
- [Basic Labels](#basic-labels)
- [Update Lifecycle Labels](#update-lifecycle-labels)
- [Version Constraint Labels](#version-constraint-labels)
- [Common Patterns](#common-patterns)
- [Label Sync](#label-sync)
- [Managing Labels](#managing-labels)

## Quick Reference

| Label | Values | Description |
|-------|--------|-------------|
| `docksmith.ignore` | `true` | Skip container from all checks and updates |
| `docksmith.allow-latest` | `true` | Allow `:latest` tag without warnings |
| `docksmith.allow-prerelease` | `true` | Include prerelease versions (alpha, beta, rc) |
| `docksmith.pre-update-check` | `/scripts/check.sh` | Script to run before updates |
| `docksmith.post-update` | `restart:name` | Action to run after updates |
| `docksmith.restart-after` | `container-name` | Restart when another container updates |
| `docksmith.auto_rollback` | `true` | Auto-rollback on health check failure |
| `docksmith.version-pin-major` | `true` | Stay within current major version |
| `docksmith.version-pin-minor` | `true` | Stay within current minor version |
| `docksmith.tag-regex` | `^v?[0-9.]+$` | Only consider matching tags |
| `docksmith.version-min` | `2.0.0` | Minimum version to consider |
| `docksmith.version-max` | `3.0.0` | Maximum version to consider |

## Basic Labels

### docksmith.ignore

Skip a container from all update checks.

```yaml
services:
  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    labels:
      - docksmith.ignore=true
```

Use for:
- Docksmith itself (prevent self-updates)
- Containers you manage manually
- Development containers

### docksmith.allow-latest

Allow `:latest` tag without migration warnings. By default, Docksmith warns about containers using `:latest` since it can't determine if updates are available.

```yaml
services:
  plex:
    image: ghcr.io/linuxserver/plex:latest
    labels:
      - docksmith.allow-latest=true
```

Use for:
- Rolling-release images you trust
- LinuxServer images that use `:latest` well
- Images with poor versioning

## Update Lifecycle Labels

### docksmith.pre-update-check

Run a script before allowing updates. Exit 0 to allow, non-zero to block.

```yaml
services:
  plex:
    image: ghcr.io/linuxserver/plex:latest
    labels:
      - docksmith.pre-update-check=/scripts/check-plex.sh
```

See [scripts.md](scripts.md) for script examples.

### docksmith.allow-prerelease

Include prerelease versions (alpha, beta, rc, dev) when checking for updates. By default, prerelease versions are skipped unless you're already running one.

```yaml
services:
  app:
    image: myapp:2.0.0
    labels:
      - docksmith.allow-prerelease=true
```

Use for:
- Testing beta releases before stable
- Applications where you want early access to features

### docksmith.post-update

Run actions after an update completes successfully.

```yaml
services:
  app:
    image: myapp:latest
    labels:
      - docksmith.post-update=restart:cache,worker
```

**Action types:**

| Type | Format | Description |
|------|--------|-------------|
| `restart` | `restart:container1,container2` | Restart containers by name |
| `compose-restart` | `compose-restart:service1` | Restart via docker compose |
| `script` | `script:/scripts/post-update.sh` | Run a script |
| `exec` | `exec:curl https://example.com/notify` | Execute a command |

**Examples:**

```yaml
# Restart related containers
- docksmith.post-update=restart:cache,worker

# Run a notification script
- docksmith.post-update=script:/scripts/notify-slack.sh

# Call a webhook
- docksmith.post-update=exec:curl -X POST https://example.com/webhook
```

### docksmith.auto_rollback

Automatically rollback if the container fails health checks after an update.

```yaml
services:
  app:
    image: myapp:latest
    labels:
      - docksmith.auto_rollback=true
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

Requires a Docker healthcheck to be configured. If the container becomes unhealthy after update, Docksmith will automatically restore the previous version.

### docksmith.restart-after

Restart this container after another container updates or restarts. Useful for VPN-dependent containers.

```yaml
services:
  gluetun:
    image: qmcgaw/gluetun:latest
    labels:
      - docksmith.allow-latest=true

  qbittorrent:
    image: linuxserver/qbittorrent:latest
    network_mode: service:gluetun
    labels:
      - docksmith.restart-after=gluetun
```

When gluetun restarts (from update or manual restart), qbittorrent automatically restarts too.

**Multiple dependencies:** Use comma-separated values:

```yaml
labels:
  - docksmith.restart-after=gluetun,vpn-helper
```

## Version Constraint Labels

### docksmith.version-pin-major

Stay within the current major version. Prevents breaking changes from major upgrades.

```yaml
services:
  postgres:
    image: postgres:16
    labels:
      - docksmith.version-pin-major=true
```

On `postgres:16.1.0`:
- ✅ Updates to `16.2.0`, `16.99.0`
- ❌ Won't update to `17.0.0`

### docksmith.version-pin-minor

Stay within the current minor version. For conservative patch-only updates.

```yaml
services:
  redis:
    image: redis:7.2
    labels:
      - docksmith.version-pin-minor=true
```

On `redis:7.2.1`:
- ✅ Updates to `7.2.2`, `7.2.99`
- ❌ Won't update to `7.3.0`

### docksmith.tag-regex

Only consider tags matching a regex pattern.

```yaml
services:
  nginx:
    image: nginx:1.25-alpine
    labels:
      - docksmith.tag-regex=^[0-9.]+-alpine$
```

Matches: `1.25.3-alpine`, `1.26.0-alpine`
Ignores: `1.25.3`, `alpine`, `mainline-alpine`

### docksmith.version-min

Set a minimum version threshold.

```yaml
services:
  node:
    image: node:20
    labels:
      - docksmith.version-min=20.0.0
```

### docksmith.version-max

Set a maximum version cap. Useful for deferring major upgrades.

```yaml
services:
  node:
    image: node:20
    labels:
      - docksmith.version-max=20.99.99
```

## Common Patterns

### Database with Major Version Pin

```yaml
services:
  postgres:
    image: postgres:16
    labels:
      - docksmith.version-pin-major=true
      - docksmith.pre-update-check=/scripts/backup-db.sh
```

### Media Server with User Check

```yaml
services:
  plex:
    image: ghcr.io/linuxserver/plex:latest
    labels:
      - docksmith.allow-latest=true
      - docksmith.pre-update-check=/scripts/check-plex.sh
```

### VPN with Dependent Containers

```yaml
services:
  gluetun:
    image: qmcgaw/gluetun:latest
    labels:
      - docksmith.allow-latest=true

  qbittorrent:
    image: linuxserver/qbittorrent:latest
    network_mode: service:gluetun
    labels:
      - docksmith.allow-latest=true
      - docksmith.restart-after=gluetun

  prowlarr:
    image: linuxserver/prowlarr:latest
    network_mode: service:gluetun
    labels:
      - docksmith.allow-latest=true
      - docksmith.restart-after=gluetun
```

### Alpine-Only Images

```yaml
services:
  nginx:
    image: nginx:1.25-alpine
    labels:
      - docksmith.tag-regex=^[0-9.]+-alpine$
```

### Stay on LTS

```yaml
services:
  node:
    image: node:20-lts
    labels:
      - docksmith.tag-regex=^20.*-lts$
```

## Label Sync

Docksmith detects when labels in your compose file differ from the running container. This happens when:
- You modify labels in the compose file but haven't recreated the container
- Labels were changed via the UI/API

The dashboard shows a "sync" indicator when labels are out of sync.

## Managing Labels

### Via Compose File

Edit your `docker-compose.yml` and recreate:

```bash
docker compose up -d
```

### Via API

```bash
# Get labels
curl http://localhost:3000/api/labels/mycontainer

# Set label
curl -X POST http://localhost:3000/api/labels/set \
  -H "Content-Type: application/json" \
  -d '{"container":"mycontainer","labels":{"docksmith.ignore":"true"}}'
```
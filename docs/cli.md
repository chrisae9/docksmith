# CLI Reference

Docksmith provides a command-line interface for all operations.

## Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Output in JSON format |
| `--help` | Show help |
| `--version` | Show version |

## Commands

### check

Check containers for available updates.

```bash
docksmith check [flags]
```

| Flag | Description |
|------|-------------|
| `--filter <name>` | Check specific container |
| `--stack <name>` | Check containers in stack |
| `--json` | JSON output |
| `--quiet` | Minimal output |

Examples:
```bash
# Check all containers
docksmith check

# Check specific container
docksmith check --filter nginx

# Check specific stack
docksmith check --stack media

# JSON output for scripting
docksmith check --json
```

### update

Update containers to newer versions.

```bash
docksmith update [flags]
```

| Flag | Description |
|------|-------------|
| `--container <name>` | Container to update |
| `--all` | Update all with available updates |
| `--stack <name>` | Update all in stack |
| `--version <ver>` | Specific version to update to |
| `--script <path>` | Override pre-update check script |
| `--dry-run` | Show what would be done |
| `--force` | Skip confirmations |

Examples:
```bash
# Update single container
docksmith update --container nginx

# Update to specific version
docksmith update --container nginx --version 1.25.0

# Update all containers with updates
docksmith update --all

# Update entire stack
docksmith update --stack media

# Dry run
docksmith update --container nginx --dry-run
```

### rollback

Rollback a previous update operation.

```bash
docksmith rollback <operation-id> [flags]
```

| Flag | Description |
|------|-------------|
| `--force` | Skip confirmation |
| `--dry-run` | Show what would be done |
| `--no-recreate` | Only restore compose file |

Examples:
```bash
# Rollback an operation
docksmith rollback op_2024011510302345

# Dry run
docksmith rollback op_2024011510302345 --dry-run

# Force without confirmation
docksmith rollback op_2024011510302345 --force

# Only restore compose file
docksmith rollback op_2024011510302345 --no-recreate
```

### restart

Restart containers with dependency awareness.

```bash
docksmith restart <container> [flags]
```

| Flag | Description |
|------|-------------|
| `--stack <name>` | Restart entire stack |
| `--force` | Skip pre-update checks |

Examples:
```bash
# Restart container
docksmith restart nginx

# Restart entire stack
docksmith restart --stack media

# Force restart (skip checks)
docksmith restart nginx --force
```

### history

View check and update history.

```bash
docksmith history [flags]
```

| Flag | Description |
|------|-------------|
| `--limit <n>` | Number of entries |
| `--container <name>` | Filter by container |

Examples:
```bash
# View recent history
docksmith history

# Limit to 10 entries
docksmith history --limit 10

# Filter by container
docksmith history --container nginx
```

### operations

View detailed update operations.

```bash
docksmith operations [flags]
```

| Flag | Description |
|------|-------------|
| `--limit <n>` | Number of entries |
| `--container <name>` | Filter by container |
| `--status <status>` | Filter by status |

Examples:
```bash
# View all operations
docksmith operations

# Filter by status
docksmith operations --status complete
docksmith operations --status failed

# Filter by container
docksmith operations --container nginx
```

### backups

List available compose file backups.

```bash
docksmith backups
```

### label

Manage container labels in compose files.

```bash
docksmith label <command> [args]
```

Subcommands:
- `get <container>` — Get labels
- `set <container> <label> <value>` — Set label
- `remove <container> <label>` — Remove label

| Flag | Description |
|------|-------------|
| `--no-restart` | Don't restart after change |
| `--force` | Skip pre-update checks |

Examples:
```bash
# Get labels
docksmith label get nginx

# Set label
docksmith label set nginx docksmith.ignore true
docksmith label set nginx docksmith.version-pin-major true

# Remove label
docksmith label remove nginx docksmith.ignore

# Set without restarting
docksmith label set nginx docksmith.ignore true --no-restart
```

### scripts

Manage pre-update check scripts.

```bash
docksmith scripts <command> [args]
```

Subcommands:
- `list` — List available scripts
- `list-assigned` — List script assignments
- `assign <container> <script>` — Assign script
- `unassign <container>` — Remove assignment

Examples:
```bash
# List scripts in /scripts directory
docksmith scripts list

# List current assignments
docksmith scripts list-assigned

# Assign script to container
docksmith scripts assign plex check-plex.sh

# Remove assignment
docksmith scripts unassign plex
```

### api

Start the API server.

```bash
docksmith api [flags]
```

| Flag | Description |
|------|-------------|
| `--port <port>` | Port to listen on (default: 8080) |
| `--static-dir <path>` | UI static files directory |

Examples:
```bash
# Start on default port
docksmith api

# Start on custom port
docksmith api --port 3000
```

---

## Common Workflows

### Check and Update All

```bash
# Check what's available
docksmith check

# Update everything
docksmith update --all
```

### Safe Database Update

```bash
# Check current version
docksmith check --filter postgres

# Dry run the update
docksmith update --container postgres --dry-run

# Update with confirmation
docksmith update --container postgres
```

### Emergency Rollback

```bash
# Find the operation ID
docksmith operations --container nginx --limit 5

# Rollback immediately
docksmith rollback op_xxx --force
```

### Scripted Updates

```bash
#!/bin/bash
# update-all.sh

# Check for updates (JSON output)
UPDATES=$(docksmith check --json | jq -r '.data.containers[] | select(.update_available) | .name')

for container in $UPDATES; do
  echo "Updating $container..."
  docksmith update --container "$container" --force
done
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error |

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `DB_PATH` | Database file path |
| `CHECK_INTERVAL` | Background check interval |
| `CACHE_TTL` | Registry cache TTL |
| `GITHUB_TOKEN` | GitHub token for GHCR |

---

## JSON Output

All commands support `--json` for machine-readable output:

```bash
docksmith check --json | jq '.data.containers[] | select(.update_available)'
```

Example output:
```json
{
  "data": {
    "containers": [...],
    "total": 25,
    "updates_available": 3
  }
}
```

Error output:
```json
{
  "error": {
    "message": "Container not found",
    "code": "NOT_FOUND"
  }
}
```

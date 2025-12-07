# Rollback

Restore containers to previous versions when updates cause issues.

## How It Works

1. Before every update, Docksmith backs up your compose file
2. If something goes wrong, rollback restores the backup
3. The container is recreated with the original image

Backups are stored alongside your compose files with timestamps.

## View Available Backups

### Web UI

Navigate to History and look for operations with rollback available.

### CLI

```bash
docksmith backups
```

Output:
```
Available backups:

Container: nginx
  Operation: op_2024011510302345
  Backup: /stacks/web/docker-compose.yml.backup.20240115103023
  From: 1.24.0 → 1.25.0
  Time: 2024-01-15 10:30:23
```

### API

```bash
curl http://localhost:8080/api/backups
```

## Perform Rollback

### Web UI

1. Go to History
2. Find the update operation
3. Click "Rollback"
4. Confirm

### CLI

```bash
# Find the operation ID
docksmith operations

# Rollback
docksmith rollback op_2024011510302345
```

### API

```bash
curl -X POST http://localhost:8080/api/rollback \
  -H "Content-Type: application/json" \
  -d '{"operation_id":"op_2024011510302345"}'
```

## Rollback Options

### Dry Run

See what would happen without making changes:

```bash
docksmith rollback op_2024011510302345 --dry-run
```

### Force (Skip Confirmation)

Skip the interactive confirmation:

```bash
docksmith rollback op_2024011510302345 --force
```

### No Recreate

Only restore the compose file, don't recreate the container:

```bash
docksmith rollback op_2024011510302345 --no-recreate
```

Use when you want to:
- Review the restored compose file first
- Manually restart at a specific time
- Handle complex dependency scenarios

## What Gets Rolled Back

| Restored | Not Restored |
|----------|--------------|
| Compose file (image tag) | Container data volumes |
| Container configuration | Database content |
| Labels and environment | External state |

Rollback changes the **image version**, not your data. Volumes persist across rollbacks.

## Rollback Process

1. **Validate** — Check backup exists and is readable
2. **Pre-rollback backup** — Save current compose file as `.before-rollback`
3. **Restore compose** — Copy backup to original location
4. **Pull image** — Download the original image if not cached
5. **Recreate container** — Stop, remove, and start with old image
6. **Health check** — Wait for container to become healthy

## Backup Storage

Backups are created at:
```
/path/to/compose/docker-compose.yml.backup.YYYYMMDDHHMMSS
```

Example:
```
/stacks/media/docker-compose.yml.backup.20240115103023
```

## Automatic Rollback

Docksmith can automatically rollback if:
- Container fails to start
- Health check fails

This requires:
1. Container has a health check defined
2. The update operation supports auto-rollback

## View Rollback History

Operations show both updates and rollbacks:

```bash
docksmith operations --status rollback
```

Or via API:
```bash
curl "http://localhost:8080/api/operations?type=rollback"
```

## Troubleshooting

### "Backup file not found"

The backup file was deleted or moved. Check:
- Backup directory permissions
- Disk space
- Manual deletions

### "Operation not found"

The operation ID is invalid or was purged. List valid operations:
```bash
docksmith backups
```

### Rollback Stuck

Check container logs:
```bash
docker logs container-name
```

Force the rollback without recreating:
```bash
docksmith rollback op_xxx --no-recreate --force
```

Then manually recreate:
```bash
docker compose up -d container-name
```

## Best Practices

1. **Test updates in staging** — Before production, test updates on non-critical containers
2. **Keep recent backups** — Don't delete backup files manually
3. **Document rollbacks** — Note why you rolled back for future reference
4. **Monitor after rollback** — Ensure the container works correctly after reverting

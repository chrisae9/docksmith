# Pre-Update Scripts

Run custom scripts before Docksmith allows updates. Block updates when conditions aren't met.

## How It Works

1. Configure a script via label: `docksmith.pre-update-check=/scripts/check.sh`
2. Before updating, Docksmith runs the script
3. **Exit 0** = Allow update
4. **Exit non-zero** = Block update

## Setup

### 1. Create Scripts Directory

```bash
mkdir scripts
```

### 2. Mount in Docksmith

```yaml
services:
  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - ./scripts:/scripts:ro
      - /home/user/stacks:/stacks:rw
```

### 3. Write Your Script

```bash
#!/bin/bash
# scripts/check-plex.sh

# Your logic here
# Exit 0 to allow, non-zero to block

exit 0
```

### 4. Make Executable

```bash
chmod +x scripts/check-plex.sh
```

### 5. Assign to Container

Via compose label:

```yaml
services:
  plex:
    image: ghcr.io/linuxserver/plex:latest
    labels:
      - docksmith.pre-update-check=/scripts/check-plex.sh
```

Or via API:

```bash
curl -X POST http://localhost:3000/api/scripts/assign \
  -H "Content-Type: application/json" \
  -d '{"container":"plex","script":"check-plex.sh"}'
```

## Script Examples

### Check Plex Active Streams

Block updates if Plex has active streams:

```bash
#!/bin/bash
# scripts/check-plex.sh

PLEX_URL="http://plex:32400"
PLEX_TOKEN="your-plex-token"

# Get active session count
SESSIONS=$(curl -s "${PLEX_URL}/status/sessions?X-Plex-Token=${PLEX_TOKEN}" \
  | grep -oP 'size="\K[0-9]+')

if [ "$SESSIONS" -gt 0 ]; then
    echo "Plex has $SESSIONS active stream(s), blocking update"
    exit 1
fi

echo "No active streams, allowing update"
exit 0
```

### Check Jellyfin Active Sessions

```bash
#!/bin/bash
# scripts/check-jellyfin.sh

JELLYFIN_URL="http://jellyfin:8096"
JELLYFIN_API_KEY="your-api-key"

SESSIONS=$(curl -s "${JELLYFIN_URL}/Sessions?api_key=${JELLYFIN_API_KEY}" \
  | jq '[.[] | select(.NowPlayingItem != null)] | length')

if [ "$SESSIONS" -gt 0 ]; then
    echo "Jellyfin has $SESSIONS active session(s), blocking update"
    exit 1
fi

exit 0
```

### Backup Database Before Update

Run a backup before allowing updates:

```bash
#!/bin/bash
# scripts/backup-postgres.sh

BACKUP_DIR="/backups/postgres"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Run pg_dump via docker exec
docker exec postgres pg_dump -U postgres mydb > "${BACKUP_DIR}/mydb_${TIMESTAMP}.sql"

if [ $? -ne 0 ]; then
    echo "Backup failed, blocking update"
    exit 1
fi

echo "Backup completed: mydb_${TIMESTAMP}.sql"
exit 0
```

### Maintenance Window Only

Only allow updates during specific hours:

```bash
#!/bin/bash
# scripts/maintenance-window.sh

HOUR=$(date +%H)

# Allow updates between 2 AM and 5 AM
if [ "$HOUR" -ge 2 ] && [ "$HOUR" -lt 5 ]; then
    echo "Within maintenance window, allowing update"
    exit 0
fi

echo "Outside maintenance window (2-5 AM), blocking update"
exit 1
```

### Check System Load

Block updates if system is under heavy load:

```bash
#!/bin/bash
# scripts/check-load.sh

LOAD=$(cat /proc/loadavg | cut -d' ' -f1)
MAX_LOAD="2.0"

if (( $(echo "$LOAD > $MAX_LOAD" | bc -l) )); then
    echo "System load ($LOAD) exceeds threshold ($MAX_LOAD)"
    exit 1
fi

exit 0
```

### Check Disk Space

Block updates if disk space is low:

```bash
#!/bin/bash
# scripts/check-disk.sh

THRESHOLD=90
USAGE=$(df / | tail -1 | awk '{print $5}' | sed 's/%//')

if [ "$USAGE" -ge "$THRESHOLD" ]; then
    echo "Disk usage ($USAGE%) exceeds threshold ($THRESHOLD%)"
    exit 1
fi

exit 0
```

### Weekday Only

Only update on weekdays:

```bash
#!/bin/bash
# scripts/weekday-only.sh

DAY=$(date +%u)

# 1-5 = Mon-Fri, 6-7 = Sat-Sun
if [ "$DAY" -ge 6 ]; then
    echo "Weekend updates disabled"
    exit 1
fi

exit 0
```

## API Reference

### List Scripts

```bash
curl http://localhost:3000/api/scripts
```

### Assign Script

```bash
curl -X POST http://localhost:3000/api/scripts/assign \
  -H "Content-Type: application/json" \
  -d '{"container":"plex","script":"check-plex.sh"}'
```

### Remove Assignment

```bash
curl -X DELETE http://localhost:3000/api/scripts/assign/plex
```

## Tips

1. **Keep scripts simple** — They run before every update attempt
2. **Use timeouts** — Curl/API calls should have reasonable timeouts
3. **Test manually** — Run your script directly to verify it works
4. **Log output** — Script stdout/stderr is captured in operation logs
5. **Mount read-only** — Scripts should be mounted `:ro` for safety
6. **Idempotent** — Scripts may run multiple times for the same update

## Troubleshooting

### Script Not Found

Check:
- Script exists in `/scripts` directory
- Filename matches exactly (case-sensitive)
- Scripts directory is mounted in Docksmith

### Permission Denied

```bash
chmod +x scripts/your-script.sh
```

### Script Blocks All Updates

Test manually:
```bash
docker exec docksmith /scripts/your-script.sh
echo $?  # Should be 0 to allow
```

### Script Output Not Visible

Check operation logs in the History page or via API:
```bash
curl http://localhost:3000/api/operations
```

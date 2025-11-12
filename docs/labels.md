# Docksmith Labels Reference

Docksmith uses Docker container labels to configure behavior for specific containers.

## Available Labels

### `docksmith.ignore`

**Purpose:** Completely ignore a container from update checking.

**Use Case:** Containers that are intentionally pinned to old versions, deprecated images, or containers where updates are known to break functionality.

**Valid Values:** `true`, `1`, `yes` (case-insensitive)

**Example:**
```yaml
services:
  readarr:
    image: ghcr.io/linuxserver/readarr:develop
    labels:
      - docksmith.ignore=true
```

**Behavior:**
- Container is skipped during discovery
- Does not appear in update reports
- Status: `IGNORED`

---

### `docksmith.allow-latest`

**Purpose:** Allow `:latest` tag without migration warnings for containers that intentionally use rolling releases.

**Use Case:** Containers where the maintainer abandoned semantic versioning and only releases via `:latest`, or containers that intentionally track edge/unstable releases.

**Valid Values:** `true`, `1`, `yes` (case-insensitive)

**Example:**
```yaml
services:
  vpn:
    image: qmcgaw/gluetun:latest
    labels:
      - docksmith.allow-latest=true
```

**Behavior Without Label:**
```
• gluetun [:latest] - UP TO DATE - MIGRATE TO SEMVER (latest)
  → Migrate to: v3.40.0
```

**Behavior With Label:**
```
• gluetun [:latest] - UP TO DATE (latest)
```

**When to Use:**
- Maintainer abandoned semantic versioning (e.g., gluetun)
- Only `:latest` is actively maintained
- Rolling release model without version tags
- Intentionally tracking development/edge builds

**Technical Details:**
- Still checks if `:latest` digest has changed (update detection works)
- Only suppresses the "MIGRATE TO SEMVER" warning
- See [edge-cases/latest-tag-handling.md](./edge-cases/latest-tag-handling.md) for details

---

### `docksmith.pre-update-check`

**Purpose:** Execute a custom script before allowing updates. If the script exits non-zero, the update is blocked.

**Use Case:** Containers where updates should be conditional on runtime state or external factors (e.g., Plex should not update while users are watching content, databases should check for active connections).

**Valid Values:** Absolute path to an executable script

**Example:**
```yaml
services:
  plex:
    image: ghcr.io/linuxserver/plex:latest
    labels:
      - docksmith.pre-update-check=/home/user/scripts/check-plex.sh
```

**Script Contract:**
- **Exit 0:** Safe to update (check passed)
- **Non-zero exit:** Block update (check failed)
- **stdout/stderr:** Captured and displayed directly as block reason (be descriptive!)
- **Best practice:** Always fail-safe (block if unable to verify conditions)

**Environment Variables Available:**
- `CONTAINER_NAME`: Name of the container being checked

**Example Check Script:**
```bash
#!/bin/bash

# Pre-update check for Plex - blocks updates if users are actively watching
# Exit 0 = safe to update (no active users)
# Exit 1 = blocked (users are watching or check failed)

cd /home/user/scripts || {
    echo "Update blocked: Cannot access check script directory"
    exit 1
}

# Check for active Plex sessions via Tautulli API
# Script outputs descriptive message and exits 0 (allow) or 1 (block)
./check-active-users.py
```

**Behavior Without Check:**
```
• plex [:latest] - UPDATE AVAILABLE (1.42.0 → 1.42.2, patch)
```

**Behavior With Check (Passed):**
```
• plex [:latest] - UPDATE AVAILABLE (1.42.0 → 1.42.2, patch)
```

**Behavior With Check (Blocked):**
```
• plex [:latest] - UPDATE AVAILABLE - BLOCKED (1.42.0 → 1.42.2, patch)
  ⚠ Blocked: Update blocked: 2 active Plex session(s) - alice, bob
```

**When to Use:**
- Update depends on runtime state (active users, connections, jobs)
- Need to check external service before updating (health checks, coordination)
- Custom business logic for update gating
- Integration with monitoring systems (Tautulli, Grafana, etc.)

**Technical Details:**
- Script is executed with `/bin/bash -c`
- Both stdout and stderr are captured
- Script timeout follows container operation timeout
- Blocked updates still appear in reports but won't be applied
- Check is only run when update is detected (not on every check)

---

## Label Format

All docksmith labels follow the format:

```
docksmith.LABEL_NAME=VALUE
```

### In docker-compose.yaml

```yaml
services:
  myservice:
    image: some/image:tag
    labels:
      - docksmith.ignore=true
      - docksmith.allow-latest=true
```

### In Dockerfile

```dockerfile
LABEL docksmith.ignore="true"
LABEL docksmith.allow-latest="true"
```

### Docker CLI

```bash
docker run -l docksmith.ignore=true some/image:tag
```

## Implementation Details

Labels are checked during container discovery:

```go
// Check for ignore label
if ignoreValue, found := container.Labels["docksmith.ignore"]; found {
    ignoreValue = strings.ToLower(strings.TrimSpace(ignoreValue))
    shouldIgnore := ignoreValue == "true" || ignoreValue == "1" || ignoreValue == "yes"
    if shouldIgnore {
        // Skip this container
    }
}

// Check for allow-latest label
if allowValue, found := container.Labels["docksmith.allow-latest"]; found {
    allowValue = strings.ToLower(strings.TrimSpace(allowValue))
    allowLatest := allowValue == "true" || allowValue == "1" || allowValue == "yes"
    if allowLatest {
        // Don't suggest migration from :latest
    }
}

// Check for pre-update check script
if checkScript, found := container.Labels["docksmith.pre-update-check"]; found && checkScript != "" {
    update.PreUpdateCheck = checkScript

    // Run the check if update is available
    success, reason := runPreUpdateCheck(ctx, checkScript, container.Name)
    if !success {
        update.Status = UpdateAvailableBlocked
        update.PreUpdateCheckFail = reason
    }
}
```

## Combining Labels

Labels can be combined:

```yaml
services:
  plex:
    image: ghcr.io/linuxserver/plex:latest
    labels:
      # Allow :latest without migration warnings
      - docksmith.allow-latest=true
      # Check for active users before allowing updates
      - docksmith.pre-update-check=/home/user/scripts/check-plex.sh

  myservice:
    image: some/image:latest
    labels:
      # This doesn't make sense - if ignored, allow-latest has no effect
      - docksmith.ignore=true
      - docksmith.allow-latest=true  # Redundant
      - docksmith.pre-update-check=/scripts/check.sh  # Also redundant
```

**Note:** If `docksmith.ignore=true` is set, the container is skipped entirely and other labels have no effect.

## Future Labels

Potential future labels (not yet implemented):

- `docksmith.update-strategy` - Control automatic update behavior
- `docksmith.pin-version` - Pin to specific version pattern
- `docksmith.check-interval` - Override default check frequency
- `docksmith.notify` - Custom notification settings
- `docksmith.allow-prerelease` - Allow upgrading to prerelease versions

## Related Documentation

- [Edge Cases: Latest Tag Handling](./edge-cases/latest-tag-handling.md)
- [USAGE.md](../USAGE.md)

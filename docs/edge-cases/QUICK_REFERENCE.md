# Edge Cases Quick Reference

Quick lookup for common edge cases and their solutions.

## By Symptom

### "UPDATE AVAILABLE" but container is actually up to date

**Possible Causes:**
1. Container uses `:latest` but maintainer abandoned semver → [latest-tag-handling.md](./latest-tag-handling.md)
2. Multi-arch digest mismatch (fixed)
3. Prerelease version being suggested (fixed)

**Quick Fix:**
```yaml
labels:
  - docksmith.allow-latest=true
```

### "UPDATE AVAILABLE (unknown)" change type

**Possible Causes:**
1. Current version cannot be parsed as semver
2. Using `:latest` tag without resolved version
3. Comparing incompatible version types

**Solution:** Usually correct behavior. If container intentionally uses `:latest`:
```yaml
labels:
  - docksmith.allow-latest=true
```

### "UP TO DATE - MIGRATE TO SEMVER" but no semver exists

**Cause:** Container uses `:latest` and docksmith suggests migrating to an old/nonexistent semver tag

**Solution:**
```yaml
labels:
  - docksmith.allow-latest=true
```

**Example:** gluetun v3.40.0 is from Dec 2024, but `:latest` is from Nov 2025

### Dev/nightly versions suggested as "latest"

**Cause:** Prerelease version tags like `2025.12.0.dev202510300239`

**Status:** Fixed in version parser (checks `strings.HasPrefix()` for prerelease identifiers)

**No Action Required**

## By Container

### gluetun (`qmcgaw/gluetun:latest`)

**Issue:** Abandoned semantic versioning after v3.40.0
**Solution:** Add `docksmith.allow-latest=true` label
**Details:** [latest-tag-handling.md](./latest-tag-handling.md)

```yaml
vpn:
  image: qmcgaw/gluetun:latest
  labels:
    - docksmith.allow-latest=true
```

### Home Assistant

**Issue:** Dev tags like `2025.12.0.dev202510300239` were suggested
**Status:** Fixed (filters prerelease versions)
**No Action Required**

### LinuxServer Images

**Issue:** Build numbers in tags (e.g., `4.0.16.2944-ls297`)
**Status:** Handled by suffix normalization
**No Action Required**

### Tailscale

**Issue:** Multi-arch digest not matching
**Status:** Fixed (includes manifest list digest)
**No Action Required**

## By Label Solution

### Use `docksmith.ignore=true` when:
- Container is intentionally pinned to old version
- Known breaking changes in updates
- Deprecated image still in use
- Updates are managed externally

### Use `docksmith.allow-latest=true` when:
- Maintainer abandoned semantic versioning
- Only `:latest` is actively maintained
- Intentionally tracking rolling releases
- Development/edge builds

## Decision Tree

```
Container shows warning/error
│
├─ Is it actually outdated?
│  ├─ Yes → Normal behavior, update when ready
│  └─ No → Continue below
│
├─ Using :latest intentionally?
│  ├─ Yes → Add docksmith.allow-latest=true
│  └─ No → Continue below
│
├─ Want to stop all checks for this container?
│  ├─ Yes → Add docksmith.ignore=true
│  └─ No → Continue below
│
└─ File an edge case issue with details:
   - Container name and image
   - Current and suggested versions
   - Output with -v flag
   - Registry URL
```

## Testing Edge Cases

### Verify Label Applied

```bash
docker inspect CONTAINER_NAME --format '{{index .Config.Labels "docksmith.allow-latest"}}'
# Should output: true
```

### Test Update Check

```bash
# Without label
./docksmith check --filter CONTAINER_NAME

# Add label to docker-compose.yaml
docker compose up -d SERVICE_NAME

# With label
./docksmith check --filter CONTAINER_NAME
```

### Check Digest Match

```bash
# Local digest
docker image inspect IMAGE:TAG --format '{{json .RepoDigests}}'

# Registry digest (requires curl/jq)
curl -s "https://hub.docker.com/v2/namespaces/NAMESPACE/repositories/REPO/tags/TAG" | jq -r '.digest'
```

## Common Patterns

### Pattern 1: Rolling Release
- **Symptom:** "MIGRATE TO SEMVER" to older version
- **Cause:** Maintainer stopped semver tagging
- **Solution:** `docksmith.allow-latest=true`

### Pattern 2: Pinned Version
- **Symptom:** Constant update notifications
- **Cause:** Intentionally staying on old version
- **Solution:** `docksmith.ignore=true`

### Pattern 3: Dev/Edge Build
- **Symptom:** Suggesting stable over your dev build
- **Cause:** Using prerelease/edge tag
- **Solution:** `docksmith.allow-latest=true` or ignore

## Need More Help?

1. Check [labels.md](../labels.md) for label details
2. Review [edge-cases/README.md](./README.md) for full list
3. Read specific edge case documentation
4. File an issue with reproduction steps

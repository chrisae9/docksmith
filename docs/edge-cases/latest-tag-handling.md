# Edge Case: Handling :latest Tag Containers

## Overview

This document describes edge cases and solutions for containers that use `:latest` or other rolling tags instead of semantic versioning.

## Problem: Gluetun - Abandoned Semantic Versioning

### Background

**Container:** `qmcgaw/gluetun:latest`
**Issue Date:** 2025-11-12
**Problem:** Container showed "UPDATE AVAILABLE (latest → v3.40.0, unknown)" even though it was up to date.

### Root Cause Analysis

The gluetun maintainer abandoned semantic versioning after v3.40.0 (December 2024) and now only releases via `:latest` tag. This created multiple issues:

#### Bug 1: Digest Resolution Not Finding :latest

**Code Location:** `/internal/update/checker.go:554-608` (`resolveVersionFromDigest()`)

**Problem:** When matching a container's digest to registry tags, the function only returned semantic version tags, ignoring `:latest` even when it matched.

```go
// OLD CODE - Would skip :latest tag
for tag, digests := range tagDigests {
    for _, digest := range digests {
        if digest == currentDigest {
            tagInfo := c.versionParser.ParseImageTag("dummy:" + tag)
            if tagInfo != nil && tagInfo.IsVersioned && tagInfo.Version != nil {
                matchingVersions = append(matchingVersions, tag)
            }
        }
    }
}
```

**Fix:** Track when `:latest` matches and return it if no semantic version found:

```go
// NEW CODE - Returns "latest" when appropriate
var matchingLatest bool
for tag, digests := range tagDigests {
    for _, digest := range digests {
        if digest == currentDigest {
            if tag == "latest" {
                matchingLatest = true
            }
            // ... semantic version logic ...
        }
    }
}

if matchingLatest {
    log.Printf("Digest matches :latest tag (no semantic version tag found)")
    return "latest"
}
```

#### Bug 2: Wrong Comparison Order

**Code Location:** `/internal/update/checker.go:281-359`

**Problem:** Code was comparing semantic versions BEFORE checking if digest changed:

1. Local `:latest` digest: `sha256:527c884262c1...` (Nov 2025)
2. Registry `:latest` digest: `sha256:527c884262c1...` (same!)
3. `findLatestVersion()` returns `v3.40.0` (Dec 2024 - older!)
4. Code compares "latest" (string) != "v3.40.0" → declares update available

This was backwards! The local image WAS up to date (digest matched), but the code found an older semantic version and incorrectly suggested "upgrading" to it.

**Fix:** Check digest FIRST for rolling tags:

```go
// Special case: If tracking a non-semantic tag (like :latest) and we have a digest,
// use digest comparison as the primary check, not fallback
if checkTag == "latest" || checkTag == "stable" || checkTag == "main" {
    if currentDigest != "" {
        log.Printf("Container %s: Using :latest tag, checking digest first", container.Name)
        // Query registry for the digest of the tag we're tracking
        latestDigest, err := c.registryManager.GetTagDigest(ctx, imageRef, checkTag)
        if err == nil {
            // Compare digests
            if currentSHA != latestSHA {
                // Digest changed - update available
                update.Status = UpdateAvailable
                // ... resolve semantic version for display ...
            } else {
                // Digests match - up to date
                update.Status = UpToDate
            }
            return update
        }
    }
}
// ... then continue with semantic version comparison for other tags ...
```

### Reality Check: Timeline

- **v3.40.0**: Released December 25, 2024
- **Local :latest**: Built November 12, 2025 (almost a year newer!)
- **Registry :latest**: Same digest as local (up to date)

The local `:latest` is receiving continuous updates via rolling releases, making it **significantly newer** than v3.40.0. Suggesting "upgrade" to v3.40.0 would actually be a **2-year downgrade**.

## Solution: `docksmith.allow-latest` Label

### Feature

Added support for `docksmith.allow-latest=true` label to indicate containers that intentionally use `:latest` and should not suggest migration to semantic versioning.

**Code Location:** `/internal/update/checker.go:181-189, 348, 482`

### Implementation

```go
// Check if container explicitly allows :latest tag
allowLatest := false
if allowValue, found := container.Labels["docksmith.allow-latest"]; found {
    allowValue = strings.ToLower(strings.TrimSpace(allowValue))
    allowLatest = allowValue == "true" || allowValue == "1" || allowValue == "yes"
    if allowLatest {
        log.Printf("Container %s has docksmith.allow-latest=true, will not suggest migration", container.Name)
    }
}

// ... later when marking as pinnable ...
if update.Status == UpToDate && checkTag == "latest" && !allowLatest {
    update.Status = UpToDatePinnable
    // ... suggest migration ...
}
```

### Usage Example

**docker-compose.yaml:**
```yaml
services:
  vpn:
    container_name: gluetun
    image: qmcgaw/gluetun:latest
    labels:
      - docksmith.allow-latest=true
```

### Behavior Comparison

**Without Label:**
```
• gluetun [:latest] - UP TO DATE - MIGRATE TO SEMVER (latest)
  → Migrate to: v3.40.0
```

**With Label:**
```
• gluetun [:latest] - UP TO DATE (latest)
```

## When to Use `docksmith.allow-latest`

Use this label when:

1. **Maintainer abandoned semantic versioning** (like gluetun)
2. **Only :latest is maintained** - no versioned releases
3. **Rolling release model** - continuous updates without tags
4. **Development/edge builds** - intentionally tracking unstable releases

## Related Labels

- `docksmith.ignore=true` - Completely ignore container (no update checks)
- `docksmith.allow-latest=true` - Allow :latest without migration warnings

## Technical Details

### Digest Comparison Flow

For containers using `:latest`, `:stable`, `:main`:

```
1. Get local image digest (from Docker RepoDigests)
2. Query registry for :latest tag digest
3. Compare digests:
   - Different → UPDATE_AVAILABLE, try to resolve semver
   - Same → UP_TO_DATE
4. If allowLatest=false and up to date:
   - Mark as UP_TO_DATE_PINNABLE
   - Suggest best available semver tag
```

### Multi-Architecture Digest Handling

**Important:** Docker's `RepoDigests` contains the manifest list digest for multi-arch images, not per-architecture digests.

**Example - Tailscale:**
- Local `RepoDigests`: `["tailscale/tailscale@sha256:abc123..."]`
- This is the manifest list digest
- Registry also returns this digest for the `:latest` tag
- Comparison works correctly

**Code Location:** `/internal/registry/dockerhub.go` - `dockerHubTag` struct includes `Digest` field for manifest list.

### Version Parser Edge Cases

**Prerelease Identification:** `/internal/version/parser.go:196-229`

Tags like `2025.12.0.dev202510300239` need special handling:

```go
// Split by separators: [2025, 12, 0, dev202510300239]
// Check if part STARTS WITH prerelease identifier
for identifier := range prereleaseIdentifiers {
    if firstPart == identifier || strings.HasPrefix(firstPart, identifier) {
        isPrerelease = true
        break
    }
}
```

This correctly identifies `dev202510300239` as a prerelease and filters it out.

## Testing

### Test Case 1: Without Label

```bash
./docksmith check --filter gluetun
```

Expected:
```
• gluetun [:latest] - UP TO DATE - MIGRATE TO SEMVER (latest)
  → Migrate to: v3.40.0
```

### Test Case 2: With Label

```bash
# Add label to docker-compose.yaml
docker compose up -d vpn

./docksmith check --filter gluetun
```

Expected:
```
• gluetun [:latest] - UP TO DATE (latest)
```

### Test Case 3: Digest Mismatch

When `:latest` points to a different digest (actual update):

Expected:
```
• gluetun [:latest] - UPDATE AVAILABLE (latest → (newer image available, tag: latest), unknown)
```

Or if a semver tag is found for the new digest:
```
• gluetun [:latest] - UPDATE AVAILABLE (latest → v3.41.0, unknown)
```

## Files Modified

- `/internal/update/checker.go:173-492` - Added label checking and digest-first logic
- `/internal/update/checker.go:554-608` - Fixed `resolveVersionFromDigest()` to return "latest"
- `/internal/registry/dockerhub.go` - Already had `Digest` field for manifest list

## Related Issues

- Home Assistant dev tag filtering: Fixed by checking `strings.HasPrefix()` for prerelease identifiers
- Tailscale version detection: Fixed by including manifest list digest in tag mappings
- Version shortening: Removed digest resolution that was shortening version tags

## Future Considerations

1. **Other rolling tags:** Consider extending to `:stable`, `:edge`, `:nightly`
2. **Auto-detection:** Could detect when no semver tags exist and auto-allow :latest
3. **Per-tag configuration:** Allow different behavior for `:edge` vs `:latest`
4. **Date-based comparison:** For containers with date-based versions, compare dates instead of strings

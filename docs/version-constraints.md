# Version Constraints

Control which versions Docksmith considers for updates.

## Overview

Version constraints filter available tags before suggesting updates. All constraints are evaluated together—a tag must pass all configured constraints.

| Constraint | Use Case |
|------------|----------|
| `version-pin-major` | Stay on PostgreSQL 16.x, won't jump to 17.x |
| `version-pin-minor` | Stay on Redis 7.2.x, won't jump to 7.3.x |
| `version-min` | Skip known-bad versions below 2.0.0 |
| `version-max` | Defer Node 21 migration, stay on 20.x |
| `tag-regex` | Only Alpine variants, only LTS tags |

## Pin Major Version

Prevents upgrades across major versions. Useful for databases and apps with breaking changes between major releases.

```yaml
services:
  postgres:
    image: postgres:16
    labels:
      - docksmith.version-pin-major=true
```

| Current | Suggested | Allowed |
|---------|-----------|---------|
| 16.1.0 | 16.2.0 | ✅ |
| 16.1.0 | 16.99.0 | ✅ |
| 16.1.0 | 17.0.0 | ❌ |

### When to Use

- **Databases** — PostgreSQL, MySQL, MongoDB major versions often require migration
- **Runtime environments** — Node.js, Python major versions may break apps
- **Breaking APIs** — Apps that change APIs between majors

## Pin Minor Version

Limits updates to patch releases only. For maximum stability.

```yaml
services:
  redis:
    image: redis:7.2
    labels:
      - docksmith.version-pin-minor=true
```

| Current | Suggested | Allowed |
|---------|-----------|---------|
| 7.2.1 | 7.2.5 | ✅ |
| 7.2.1 | 7.3.0 | ❌ |
| 7.2.1 | 8.0.0 | ❌ |

### When to Use

- Production databases needing only security patches
- Mission-critical services
- When you want to manually review minor/major updates

## Version Minimum

Skip versions below a threshold. Useful when older versions have known issues.

```yaml
services:
  myapp:
    image: myapp:2.5.0
    labels:
      - docksmith.version-min=2.0.0
```

| Available | Considered |
|-----------|------------|
| 1.9.0 | ❌ |
| 2.0.0 | ✅ |
| 2.5.0 | ✅ |
| 3.0.0 | ✅ |

### When to Use

- Skip deprecated version branches
- Avoid versions with known security issues
- Ensure minimum feature set

## Version Maximum

Cap versions at a threshold. Useful for deferring major migrations.

```yaml
services:
  node:
    image: node:20.10.0
    labels:
      - docksmith.version-max=20.99.99
```

| Available | Considered |
|-----------|------------|
| 20.10.0 | ✅ |
| 20.15.0 | ✅ |
| 21.0.0 | ❌ |
| 22.0.0 | ❌ |

### When to Use

- Defer major version migrations
- Stay on LTS while newer versions exist
- Avoid breaking changes in newer versions

## Tag Regex

Filter tags by pattern. Most flexible option for complex filtering.

```yaml
services:
  nginx:
    image: nginx:1.25-alpine
    labels:
      - docksmith.tag-regex=^[0-9.]+-alpine$
```

### Common Patterns

**Alpine images only:**
```yaml
labels:
  - docksmith.tag-regex=^[0-9.]+-alpine$
```
Matches: `1.25.3-alpine`, `1.26-alpine`
Ignores: `1.25.3`, `alpine`, `1.25-bookworm`

**Semantic versions only:**
```yaml
labels:
  - docksmith.tag-regex=^v?[0-9]+\.[0-9]+\.[0-9]+$
```
Matches: `1.2.3`, `v1.2.3`
Ignores: `latest`, `dev`, `1.2.3-rc1`

**LTS tags:**
```yaml
labels:
  - docksmith.tag-regex=^[0-9]+-lts$
```
Matches: `20-lts`, `18-lts`
Ignores: `20`, `20.10.0`, `current`

**Specific architecture:**
```yaml
labels:
  - docksmith.tag-regex=^[0-9.]+-amd64$
```

**Exclude pre-releases:**
```yaml
labels:
  - docksmith.tag-regex=^v?[0-9]+\.[0-9]+\.[0-9]+$
```
Ignores: `1.0.0-beta`, `1.0.0-rc1`, `1.0.0-alpha`

## Combining Constraints

Constraints combine with AND logic. A tag must pass all constraints.

### Major Pin + Regex

Stay on Node 20.x with only LTS tags:

```yaml
services:
  node:
    image: node:20-lts
    labels:
      - docksmith.version-pin-major=true
      - docksmith.tag-regex=^[0-9]+-lts$
```

### Min + Max Range

Only allow versions between 2.0 and 3.0:

```yaml
services:
  myapp:
    image: myapp:2.5.0
    labels:
      - docksmith.version-min=2.0.0
      - docksmith.version-max=2.99.99
```

### Major Pin + Regex (Alpine)

Stay on PostgreSQL 16.x with Alpine images:

```yaml
services:
  postgres:
    image: postgres:16-alpine
    labels:
      - docksmith.version-pin-major=true
      - docksmith.tag-regex=^[0-9.]+-alpine$
```

## Edge Cases

### Date-Based Versions

Some images use date-based tags like `2024.01.15`. Docksmith understands these:

```yaml
services:
  calibre-web:
    image: linuxserver/calibre-web:2024.01.15
    labels:
      # Pin to 2024.x releases
      - docksmith.tag-regex=^2024\.[0-9.]+$
```

### LinuxServer Build Numbers

LinuxServer images often have build suffixes like `1.0.0-ls123`. Docksmith strips these for comparison:

- `1.0.0-ls123` → compares as `1.0.0`
- `1.0.1-ls124` → compares as `1.0.1`

No special configuration needed.

### Rolling Tags

For rolling tags like `:latest`, `:stable`, `:edge`:

```yaml
services:
  myapp:
    image: myapp:stable
    labels:
      - docksmith.allow-latest=true
```

Docksmith checks by digest, not tag, so it detects when the underlying image changes.

## Testing Constraints

Use the Tag Filter page in the UI to test your regex patterns:

1. Navigate to a container
2. Click "Tag Filter"
3. Enter your regex pattern
4. See which tags match/don't match

Or via API:

```bash
curl "http://localhost:8080/api/registry/tags/nginx" | jq
```

## Best Practices

1. **Databases**: Always use `version-pin-major` for PostgreSQL, MySQL, MongoDB
2. **Production**: Consider `version-pin-minor` for critical services
3. **Alpine**: Use `tag-regex` if you specifically want Alpine variants
4. **Defer migrations**: Use `version-max` to stay on current major while planning upgrade
5. **Test regex**: Use the Tag Filter page before applying patterns

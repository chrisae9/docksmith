# Version Edge Case Test Suite

## Overview

This directory contains JSON-based test cases for validating semantic version parsing and update logic across popular Docker containers with various versioning patterns.

## Coverage: 94% (17/18 tests passing)

### ✅ Fully Passing Containers

| Container | Tests | Status | Features Tested |
|-----------|-------|--------|-----------------|
| **alpine** | 2/2 | ✅ | Date-based (YYYYMMDD) + semantic versions |
| **linuxserver** | 3/3 | ✅ | Build numbers (-ls250), git hashes, normalization |
| **nginx** | 2/2 | ✅ | Combined suffixes (alpine-perl), meta tags |
| **node** | 3/3 | ✅ | LTS codenames, major version pinning, major-only tags |
| **postgres** | 3/3 | ✅ | Alpine family matching (alpine3.18 ↔ alpine3.19), Debian codenames, major version pinning |
| **redis** | 2/2 | ✅ | Suffix matching, major version pinning |
| **traefik** | 2/2 | ✅ | v-prefix normalization, prerelease versions (beta, rc) |

### ❌ Expected Failure

| Container | Tests | Status | Reason |
|-----------|-------|--------|--------|
| **gluetun** | 0/1 | ❌ | Rolling release model (digest-based comparison required) |

## Running Tests

```bash
# Run all edge case tests
go test -v ./internal/version -run TestVersionEdgeCases

# Run specific container tests
go test -v ./internal/version -run TestVersionEdgeCases/node

# Check parser coverage
go test -v ./internal/version -run TestParserCoverage

# Generate edge case documentation
go test -v ./internal/version -run TestEdgeCaseDocumentation
```

## Adding New Test Cases

### Quick Start

1. **Create a JSON file** in this directory or `custom/`:
```bash
cat > custom/mycontainer.json << 'EOF'
{
  "name": "namespace/mycontainer",
  "description": "Brief description of versioning pattern",
  "docker_hub_url": "https://hub.docker.com/r/namespace/mycontainer",
  "test_cases": [
    {
      "image": "namespace/mycontainer",
      "current_tag": "1.2.3-alpine",
      "available_tags": ["1.2.3-alpine", "1.2.4-alpine", "1.3.0-alpine"],
      "expected_latest": "1.3.0-alpine",
      "expected_version": "1.3.0",
      "notes": "What should happen"
    }
  ]
}
EOF
```

2. **Run the test**:
```bash
go test -v ./internal/version -run TestVersionEdgeCases/mycontainer
```

3. **Fix or document** based on results

### Using Labels

Test cases can include Docker labels to test label-based behavior:

```json
{
  "image": "node",
  "current_tag": "20.10.0-alpine",
  "available_tags": ["20.10.0-alpine", "20.11.1-alpine3.19", "21.0.0-alpine"],
  "expected_latest": "20.11.1-alpine3.19",
  "expected_version": "20.11.1",
  "notes": "Stay in major version 20",
  "labels": {
    "docksmith.version-pin-major": "true"
  }
}
```

## Supported Labels

| Label | Type | Description |
|-------|------|-------------|
| `docksmith.version-pin-major` | boolean | Pin updates within current major version (e.g., Node 20.x won't upgrade to 21.x) |
| `docksmith.allow-latest` | boolean | Allow tracking of `:latest` tags (for rolling release containers) |

## JSON Schema

### Required Fields

```json
{
  "name": "string",           // Container name/namespace
  "test_cases": [             // Array of test scenarios
    {
      "image": "string",           // Full image name
      "current_tag": "string",     // Tag currently running
      "available_tags": ["..."],   // Array of tags to compare
      "expected_latest": "string", // Which tag should be chosen
      "expected_version": "string",// Extracted version number
      "notes": "string"            // Human-readable explanation
    }
  ]
}
```

### Optional Fields

```json
{
  "description": "string",        // What makes this versioning special
  "docker_hub_url": "string",     // Link to Docker Hub
  "edge_cases": [                 // Array of edge case documentation
    {
      "issue": "string",               // What's the problem
      "example": "string",             // Real example
      "expected_behavior": "string",   // What should happen
      "requires_override": false       // Boolean
    }
  ],
  "test_cases": [
    {
      "labels": {                      // Docker labels for testing
        "label.key": "value"
      }
    }
  ]
}
```

## Parser Features Tested

### ✅ Implemented and Tested

- **Semantic versioning**: `1.2.3`, `2.10.5`
- **Major-only tags**: `15`, `20` (parsed as `15.0.0`, `20.0.0`)
- **Major.minor tags**: `7.2`, `3.1` (parsed as `7.2.0`, `3.1.0`)
- **Version prefixes**: `v1.2.3` → `1.2.3`
- **Date-based versions**: `20240115`, `2024.01.15`
- **Prerelease versions**: `alpha`, `beta`, `rc`, `dev`
- **Build number normalization**: Remove `-ls250`, git hashes, timestamps
- **Suffix matching**:
  - Exact match: `alpine` = `alpine`
  - Prefix match: `alpine` matches `alpine3.19`
  - Family match: `alpine3.18` matches `alpine3.19` (same family)
- **Major version pinning**: Stay within current major version when label is set
- **Suffix preference**: Prefer suffixed tags when current has suffix

### ❌ Not Implemented (Requires Override)

- **Digest-based comparison**: For rolling release containers (like gluetun)
- **Custom version patterns**: Container-specific versioning schemes
- **Minor version pinning**: Stay within major.minor (would need new label)

## Common Versioning Patterns

| Pattern | Example | Parser Support |
|---------|---------|----------------|
| Semantic | `1.2.3`, `2.10.5` | ✅ Full support |
| Major-only | `15`, `20` | ✅ Parsed as `X.0.0` |
| Major.minor | `7.2`, `3.1` | ✅ Parsed as `X.Y.0` |
| Calver YYYY.MM.patch | `2024.11.1` | ✅ Parsed as semver |
| Date YYYYMMDD | `20241115` | ✅ Date-based parsing |
| Build numbers | `1.2.3-ls245` | ✅ Normalized out |
| Git hashes | `1.2.3-abc123` | ✅ Normalized out |
| v-prefix | `v1.2.3` | ✅ Stripped |
| LTS codenames | `20-iron`, `18-hydrogen` | ✅ Treated as suffix |
| Platform variants | `alpine3.19`, `bookworm` | ✅ Smart matching |
| Rolling release | `:latest` only | ❌ Needs digest comparison |

## Tips for AI-Assisted Debugging

When a container isn't working correctly:

1. **Capture real Docker Hub tags** - Use actual tags from the container registry
2. **Add detailed notes** - Explain the versioning scheme clearly
3. **Document edge cases** - Even if they work, document unusual patterns
4. **Link to documentation** - If the project documents versioning
5. **One file per pattern** - Easier to find and maintain

## Example: Complete Test Suite

See `node.json` for a comprehensive example showing:
- LTS codenames (iron, hydrogen)
- Major version pinning
- Combined suffixes (iron-alpine)
- Major-only tags (`20`)
- Edge case documentation

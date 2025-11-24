# Custom Container Version Test Cases

This directory is for adding test cases for containers with versioning edge cases that aren't covered by the standard test suites.

## Quick Start

When someone reports "My container XYZ isn't working", follow these steps:

### 1. Create a JSON test file

```bash
# Create a new JSON file named after the container
touch custom/mycontainer.json
```

### 2. Add test cases with real tags

```json
{
  "name": "namespace/mycontainer",
  "description": "Brief description of versioning pattern",
  "docker_hub_url": "https://hub.docker.com/r/namespace/mycontainer",
  "test_cases": [
    {
      "image": "namespace/mycontainer",
      "current_tag": "1.2.3-weird-suffix",
      "available_tags": [
        "1.2.3-weird-suffix",
        "1.2.4-weird-suffix",
        "1.3.0-weird-suffix",
        "latest"
      ],
      "expected_latest": "1.3.0-weird-suffix",
      "expected_version": "1.3.0",
      "notes": "Explain what should happen"
    }
  ],
  "edge_cases": [
    {
      "issue": "Brief description of the problem",
      "example": "Actual tag example",
      "expected_behavior": "What should happen",
      "requires_override": false
    }
  ]
}
```

### 3. Run the test

```bash
cd /home/chis/www/docksmith
go test -v ./internal/version -run EdgeCase/mycontainer
```

### 4. Fix if needed

**If test fails:**
- ✅ Try adding an override (see below)
- ✅ If override doesn't work, the parser may need enhancement
- ✅ Ask AI to analyze the failure and suggest fixes

**If test passes:**
- ✅ Great! The current parser handles it
- ✅ Commit the test case to prevent regressions

## Override Options

If the standard parser can't handle the versioning, add an override:

### Option 1: Label-based Override (Recommended)

Add to your `docker-compose.yml`:

```yaml
services:
  mycontainer:
    image: namespace/mycontainer:1.2.3-weird
    labels:
      docksmith.version-pattern: "semver"  # or "date-YYYYMMDD", "digest-only"
      docksmith.version-prefix: "v"
      docksmith.version-suffix-required: "weird"
```

### Option 2: Embedded Override

Add to `internal/version/overrides.go`:

```go
var KnownEdgeCases = map[string]VersionConfig{
    "namespace/mycontainer": {
        Pattern: Semver,
        StripPrefix: "v",
        RequiredSuffix: "weird",
        Notes: "Brief explanation",
    },
}
```

### Option 3: Configuration File

Create/edit `/data/version-overrides.json`:

```json
{
  "overrides": {
    "namespace/mycontainer": {
      "pattern": "semver",
      "strip_prefix": "v",
      "required_suffix": "weird",
      "reason": "Explanation of why override is needed"
    }
  }
}
```

## JSON Schema Reference

### Required Fields

- `name` - Container name/namespace
- `test_cases` - Array of test scenarios
  - `image` - Full image name
  - `current_tag` - Tag currently running
  - `available_tags` - Array of tags to compare against
  - `expected_latest` - Which tag should be chosen as latest
  - `expected_version` - Extracted version number
  - `notes` - Human-readable explanation

### Optional Fields

- `description` - What makes this container's versioning special
- `docker_hub_url` - Link to Docker Hub for reference
- `edge_cases` - Array of edge case documentation
  - `issue` - What's the problem
  - `example` - Real example
  - `expected_behavior` - What should happen
  - `requires_override` - Boolean, does this need special handling
- `override_config` - Suggested override configuration
- `normalization_rules` - Regex patterns for normalization

## Example: Real-World Case

```json
{
  "name": "homeassistant/home-assistant",
  "description": "Home Assistant with YY.MM.patch Calver",
  "docker_hub_url": "https://hub.docker.com/r/homeassistant/home-assistant",
  "test_cases": [
    {
      "image": "homeassistant/home-assistant",
      "current_tag": "2024.11.1",
      "available_tags": [
        "2024.11.1",
        "2024.11.2",
        "2024.12.0",
        "2025.1.0",
        "dev"
      ],
      "expected_latest": "2025.1.0",
      "expected_version": "2025.1.0",
      "notes": "Uses YYYY.MM.patch Calver, should compare as dates"
    }
  ],
  "edge_cases": [
    {
      "issue": "Calendar versioning (Calver)",
      "example": "2024.11.1 means November 2024, patch 1",
      "expected_behavior": "Compare as semantic version, not date-based",
      "notes": "Dotted format is semantic enough to work"
    }
  ]
}
```

## Tips for AI-Assisted Debugging

1. **Include actual Docker Hub tags** - Makes it easy to verify
2. **Add detailed notes** - Explain the versioning scheme
3. **Document edge cases** - Even if they work, document why
4. **Link to documentation** - If the project documents their versioning
5. **One file per container pattern** - Easier to find and fix

## Common Patterns

| Pattern | Example | Notes |
|---------|---------|-------|
| Calver YYYY.MM.patch | `2024.11.1` | Parsed as semver |
| Calver YY.MM | `24.11` | Parsed as semver 24.11.0 |
| Date YYYYMMDD | `20241115` | Parsed as date |
| Build numbers | `1.2.3-ls245` | Normalized out |
| Git hashes | `1.2.3-abc123` | Normalized out |
| Prefixes | `v1.2.3`, `r1.2.3` | Stripped |
| LTS codenames | `20-iron` | Treated as suffix |
| Platform variants | `alpine3.19` | Treated as suffix |

## Getting Help

If you're stuck:

1. Run the test with `-v` for verbose output
2. Check existing test files for similar patterns
3. Look at `internal/version/parser.go` to see what's supported
4. Ask in GitHub Issues with your JSON test case

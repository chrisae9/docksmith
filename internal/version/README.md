# Version Parsing Documentation

## Overview

The version parsing system in Docksmith is designed to handle the wide variety of version tag formats found in real-world Docker images. It has been empirically tested against popular images like nginx, postgres, and redis.

## Supported Version Formats

### 1. Semantic Versioning (SemVer)

The most common format following `MAJOR.MINOR.PATCH` convention.

**Examples:**
- `1.25.3` - Full semantic version
- `16.1` - Major.minor only (patch defaults to 0)
- `16` - Major only (minor and patch default to 0)
- `v2.9.6` - With 'v' prefix
- `1.0.0-beta.1` - With prerelease identifier
- `1.21.3-alpine` - With platform suffix

**Parsing Rules:**
- Optional `v` prefix is stripped
- Missing components default to 0
- Prerelease identifiers (alpha, beta, rc, dev, pre, preview, canary) are captured separately
- Platform suffixes (alpine, slim, bookworm, etc.) are extracted as suffixes
- Build metadata is normalized/removed (e.g., `-ls285`, `-r3`)

### 2. Date-Based Versions

Some images use date-based versioning instead of semantic versions.

**Supported Formats:**
- `2024.01.15` (YYYY.MM.DD with dots)
- `2024-01-15` (YYYY-MM-DD with dashes)
- `20240115` (YYYYMMDD compact)

**Parsing Rules:**
- Dates are checked BEFORE semantic versions to avoid confusion
- Date components are mapped to version fields: Year→Major, Month→Minor, Day→Patch
- This allows date-based versions to be compared using the same logic as semantic versions
- Single-digit dates (e.g., `2024.1.5`) are parsed as semantic versions, not dates

### 3. Commit Hash Versions

Git commit hashes used as version identifiers.

**Examples:**
- `abc123d` - Short hash (7 chars)
- `abc123def456789012345678901234567890abcd` - Long hash (40 chars)
- `sha256-abc123def456` - With prefix

**Parsing Rules:**
- Detected by pattern matching for hex strings
- Not truly "versioned" in the comparable sense
- Stored as-is without parsing

### 4. Meta Tags

Special tags that don't represent specific versions.

**Recognized Meta Tags:**
- `latest` - Default/latest stable version
- `stable` - Stable release channel
- `main`, `master` - Main branch builds
- `develop`, `dev` - Development builds
- `edge` - Cutting edge/experimental
- `nightly` - Nightly builds
- `beta`, `alpha`, `rc` - Pre-release channels

## Version Components

### Core Version

- **Major**: Breaking changes
- **Minor**: New features, backwards compatible
- **Patch**: Bug fixes

### Prerelease

Indicates pre-release versions that come before the stable release.

**Common Identifiers:**
- `alpha` - Early testing
- `beta` - Feature complete, testing
- `rc` - Release candidate
- `dev` - Development snapshot
- `pre` - Pre-release

**Rules:**
- Prerelease versions are LESS than release versions
- `1.0.0-beta` < `1.0.0`
- Prerelease comparison is lexical

### Suffix

Platform or variant identifiers that don't affect version comparison.

**Common Suffixes:**
- `alpine` - Alpine Linux base
- `alpine3.18`, `alpine3.19` - Specific Alpine versions
- `slim` - Minimal image variant
- `bookworm`, `bullseye` - Debian release names
- `perl` - With Perl support
- `tensorrt` - With TensorRT support

**Rules:**
- Suffixes are extracted but NOT compared
- `1.25.3` equals `1.25.3-alpine` in version comparison
- Different suffixes don't affect version ordering
- Build metadata patterns are normalized out (`-ls285`, `-r3`, etc.)

## Version Comparison

### Comparison Logic

Versions are compared component by component:

1. Compare Major versions
2. If equal, compare Minor versions
3. If equal, compare Patch versions
4. If equal, compare Prerelease (no prerelease > with prerelease)

### Change Types

- **MajorChange**: Major version increased (1.0.0 → 2.0.0)
- **MinorChange**: Minor version increased (1.0.0 → 1.1.0)
- **PatchChange**: Patch version increased (1.0.0 → 1.0.1)
- **Downgrade**: Version decreased
- **NoChange**: Versions are equal

### Suffix Handling in Comparisons

**Important**: Suffixes do NOT affect version comparison!

```
1.25.3 == 1.25.3-alpine == 1.25.3-alpine3.18 == 1.25.3-slim
```

This is intentional because:
- Suffixes represent platform/variant choices, not version differences
- You want to know if the base version changed, regardless of platform
- Allows flexible platform switching without false update detection

## Empirical Testing Results

### Test Coverage

The parser has been tested with real-world tags from:
- **nginx**: 1.25.3, 1.25.3-alpine, 1.25.3-alpine3.18, 1.25.3-perl, 1.25.3-bookworm
- **postgres**: 16, 16.1, 16.1-alpine, 16.1-alpine3.19, 16.1-bullseye, 9.6.24
- **redis**: 7.2.3, 7.2.3-alpine, 7.2.3-alpine3.19, 7.2.3-bookworm, 7.2

### Known Limitations

1. **Single-digit dates**: `2024.1.5` is parsed as semantic version, not a date
   - Workaround: Use zero-padded dates `2024.01.05`

2. **Complex prerelease formats**: Only common prerelease identifiers are recognized
   - Unrecognized prelease identifiers become part of the suffix

3. **Build metadata**: Normalized out by default
   - LinuxServer builds: `5.28.0.10274-ls285` → suffix becomes `10274`
   - This is intentional to focus on version, not build number

4. **Date vs Semantic ambiguity**: Very high year-like versions (e.g., `2024.1.2`) are parsed as dates
   - This is by design - dates are checked first

## Usage Examples

### Basic Parsing

```go
parser := version.NewParser()

// Parse a full image tag
info := parser.ParseImageTag("nginx:1.25.3-alpine")
fmt.Println(info.Version.Major)    // 1
fmt.Println(info.Version.Minor)    // 25
fmt.Println(info.Version.Patch)    // 3
fmt.Println(info.Suffix)            // "alpine"
fmt.Println(info.IsVersioned)      // true
fmt.Println(info.VersionType)      // "semantic"

// Parse just a tag
ver := parser.ParseTag("1.25.3")
fmt.Println(ver.Major)              // 1
```

### Version Comparison

```go
comparator := version.NewComparator()

v1 := parser.ParseTag("1.24.0")
v2 := parser.ParseTag("1.25.0")

result := comparator.Compare(v1, v2)  // -1 (v1 < v2)
isNewer := comparator.IsNewer(v1, v2) // true
changeType := comparator.GetChangeType(v1, v2) // MinorChange
```

### Handling Suffixes

```go
// Suffixes are available but don't affect comparison
info1 := parser.ParseImageTag("nginx:1.25.3")
info2 := parser.ParseImageTag("nginx:1.25.3-alpine")

result := comparator.Compare(info1.Version, info2.Version) // 0 (equal)

// But you can still check the suffix for display purposes
fmt.Println(info2.Suffix) // "alpine"
```

### Date-Based Versions

```go
info := parser.ParseImageTag("myapp:2024.01.15")
fmt.Println(info.VersionType)       // "date"
fmt.Println(info.Version.Major)     // 2024
fmt.Println(info.Version.Minor)     // 1
fmt.Println(info.Version.Patch)     // 15

// Dates can be compared like semantic versions
v1 := parser.ParseTag("2024.01.15")
v2 := parser.ParseTag("2024.02.20")
result := comparator.Compare(v1, v2) // -1 (earlier date)
```

## Troubleshooting

### "My version isn't being parsed correctly"

1. Check if it matches one of the supported formats
2. Look at the `VersionType` field to see how it was classified
3. Review the empirical test cases for similar examples
4. Consider if your format needs special handling

### "Suffixes are being lost"

Suffixes are being properly extracted but might be normalized:
- Build numbers like `-ls285` are removed
- Check the `normalizeSuffix()` function for patterns that are filtered out

### "Comparison isn't working as expected"

Remember:
- Suffixes don't affect comparison
- Prerelease versions come BEFORE release versions
- Date-based versions use year/month/day as major/minor/patch

## Performance Considerations

- Parsing is fast - uses compiled regex patterns
- Caching is recommended for repeated parsing of the same tags
- Version comparison is simple integer comparison

## Future Enhancements

Potential improvements based on empirical testing:
- Support for CalVer (calendar versioning) variants
- Configurable suffix filtering
- Custom prerelease identifier recognition
- Extended date format support

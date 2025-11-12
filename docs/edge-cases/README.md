# Edge Cases Documentation

This directory contains documentation for edge cases encountered during container update checking, their root causes, and solutions.

## Quick Start

→ **[Quick Reference Guide](./QUICK_REFERENCE.md)** - Fast lookup for common issues and solutions

## Index

### Container-Specific Edge Cases

1. **[Latest Tag Handling](./latest-tag-handling.md)** - Containers that use `:latest` instead of semantic versioning
   - **Container:** gluetun (`qmcgaw/gluetun:latest`)
   - **Issue:** Abandoned semantic versioning, false update detection
   - **Solution:** `docksmith.allow-latest=true` label + digest-first comparison
   - **Date:** 2025-11-12

## Categories

### By Problem Type

- **Rolling Releases:** [latest-tag-handling.md](./latest-tag-handling.md)
- **Version Detection:** (TBD)
- **Registry Issues:** (TBD)
- **Multi-Architecture:** Covered in [latest-tag-handling.md](./latest-tag-handling.md#multi-architecture-digest-handling)

### By Container

- **gluetun:** [latest-tag-handling.md](./latest-tag-handling.md)

## Contributing New Edge Cases

When documenting a new edge case, include:

1. **Overview** - Brief description of the problem
2. **Background** - Container name, image, when discovered
3. **Root Cause Analysis** - Technical explanation with code references
4. **Solution** - Code changes and implementation
5. **Usage** - How to handle similar cases
6. **Testing** - Test cases and expected behavior
7. **Files Modified** - List of changed files with line numbers

## Labels Reference

### Available Labels

- `docksmith.ignore=true` - Skip update checking entirely
- `docksmith.allow-latest=true` - Allow `:latest` tag without migration warnings

### Label Format

```yaml
labels:
  - docksmith.LABEL=VALUE
```

Valid boolean values: `true`, `1`, `yes` (case-insensitive)

## Related Documentation

- [USAGE.md](../../USAGE.md) - General usage documentation
- [FIXES_UPDATE_ORCHESTRATOR.md](../../FIXES_UPDATE_ORCHESTRATOR.md) - Update orchestrator fixes
- [REGISTRY_ERROR_HANDLING_FIX.md](../../REGISTRY_ERROR_HANDLING_FIX.md) - Registry error handling

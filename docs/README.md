# Docksmith Documentation

Welcome to the Docksmith documentation. This directory contains detailed documentation for various aspects of the project.

## Quick Links

- **[Labels Reference](./labels.md)** - Guide to all available container labels
- **[Edge Cases](./edge-cases/)** - Known edge cases and their solutions

## Documentation Structure

```
docs/
├── README.md                      # This file
├── labels.md                      # Container labels reference
└── edge-cases/                    # Edge case documentation
    ├── README.md                  # Edge cases index
    └── latest-tag-handling.md     # Rolling release containers
```

## Getting Started

### Basic Usage

For general usage, see [USAGE.md](../USAGE.md) in the project root.

### Quick Command Reference

```bash
# Check all containers
docksmith check

# Check specific container
docksmith check --filter nginx

# Check specific stack
docksmith check --stack torrent

# Output as JSON
docksmith check --json=true

# Verbose output
docksmith check -v
```

## Container Labels

Docksmith uses Docker labels to configure behavior:

- `docksmith.ignore=true` - Skip update checking
- `docksmith.allow-latest=true` - Allow `:latest` without warnings

See [labels.md](./labels.md) for complete reference.

## Common Edge Cases

### Containers Using :latest Tag

Some containers (like gluetun) abandoned semantic versioning and only release via `:latest`. Use the `docksmith.allow-latest=true` label to prevent migration warnings.

**Documentation:** [edge-cases/latest-tag-handling.md](./edge-cases/latest-tag-handling.md)

### Deprecated Containers

For containers you intentionally keep on old versions, use `docksmith.ignore=true` to skip update checking entirely.

## Project Structure

```
docksmith/
├── cmd/
│   └── docksmith/          # CLI entry point
├── internal/
│   ├── docker/             # Docker client wrapper
│   ├── registry/           # Container registry APIs
│   │   ├── dockerhub.go    # Docker Hub implementation
│   │   └── ghcr.go         # GitHub Container Registry
│   ├── storage/            # Database/cache layer
│   ├── update/             # Update checking logic
│   │   ├── checker.go      # Core update checker
│   │   ├── orchestrator.go # Stack/container orchestration
│   │   └── types.go        # Type definitions
│   └── version/            # Version parsing/comparison
│       ├── parser.go       # Tag parsing
│       └── comparator.go   # Version comparison
├── docs/                   # Documentation (this folder)
└── test/                   # Tests
```

## Development

### Key Components

1. **Version Parser** (`internal/version/parser.go`)
   - Parses Docker image tags
   - Handles semantic versions, date-based versions, and commit hashes
   - Filters prerelease versions

2. **Update Checker** (`internal/update/checker.go`)
   - Checks individual containers for updates
   - Handles digest comparison for rolling releases
   - Respects container labels

3. **Registry Manager** (`internal/registry/`)
   - Interfaces with Docker Hub and GHCR
   - Fetches tags and digests
   - Handles rate limiting and errors

4. **Orchestrator** (`internal/update/orchestrator.go`)
   - Discovers containers and stacks
   - Coordinates update checks
   - Manages caching

### Adding New Edge Cases

When you discover a new edge case:

1. Fix the issue in code
2. Document in `docs/edge-cases/`
3. Update `docs/edge-cases/README.md` index
4. Add test cases if applicable

Template for edge case documentation:
- **Overview** - Brief description
- **Background** - Container details, discovery date
- **Root Cause** - Technical analysis with code refs
- **Solution** - Implementation details
- **Usage** - How to handle similar cases
- **Testing** - Test cases and expected results
- **Files Modified** - Changed files with line numbers

## Historical Documentation

These documents in the project root contain historical fixes and context:

- `FIXES_UPDATE_ORCHESTRATOR.md` - Update orchestrator improvements
- `REGISTRY_ERROR_HANDLING_FIX.md` - Registry error handling fixes
- `INTEGRATION_TEST_RESULTS.md` - Integration test results

## Contributing

When adding documentation:

1. Place container-specific docs in `docs/edge-cases/`
2. Place feature docs in `docs/`
3. Update relevant README indexes
4. Use markdown with code examples
5. Include file paths and line numbers for code references

## Support

For issues or questions:
- Check existing edge case documentation
- Review label reference
- Consult general usage guide
- File an issue on GitHub

# Registry Configuration

Docksmith supports Docker Hub, GitHub Container Registry (GHCR), and private registries.

## Contents

- [Docker Hub](#docker-hub)
- [GitHub Container Registry (GHCR)](#github-container-registry-ghcr)
- [Private Registries](#private-registries)
- [Caching](#caching)
- [LinuxServer Images](#linuxserver-images)
- [Multi-Architecture Images](#multi-architecture-images)
- [Troubleshooting](#troubleshooting)

## Docker Hub

### Public Images

No configuration needed. Docksmith checks Docker Hub automatically.

```yaml
services:
  nginx:
    image: nginx:1.25
```

### Rate Limits

Docker Hub limits anonymous requests to 100 pulls per 6 hours per IP. To increase limits, authenticate.

### Authenticated Access

Mount your Docker config for authenticated access:

```yaml
services:
  docksmith:
    volumes:
      - ~/.docker/config.json:/root/.docker/config.json:ro
```

Login on the host first:
```bash
docker login
```

This increases rate limits and enables private image access.

## GitHub Container Registry (GHCR)

### Public Images

Works automatically:

```yaml
services:
  plex:
    image: ghcr.io/linuxserver/plex:latest
```

### Private Images

Login to GHCR and mount your docker config:

```bash
# Login on the host
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin
```

```yaml
services:
  docksmith:
    volumes:
      - ~/.docker/config.json:/root/.docker/config.json:ro
```

Your GitHub token needs the `read:packages` scope.

## Private Registries

### With Docker Config

If you've logged into a private registry:

```bash
docker login registry.example.com
```

Mount the config:

```yaml
services:
  docksmith:
    volumes:
      - ~/.docker/config.json:/root/.docker/config.json:ro
```

### Registry Types Supported

| Registry | Supported |
|----------|-----------|
| Docker Hub | ✅ |
| GitHub (ghcr.io) | ✅ |
| GitLab Registry | ✅ |
| AWS ECR | ✅ (with credentials helper) |
| Google GCR | ✅ (with credentials helper) |
| Azure ACR | ✅ (with credentials helper) |
| Harbor | ✅ |
| Self-hosted | ✅ |

### HTTP Registries

For registries without TLS (not recommended for production):

```bash
# Add to Docker daemon config
# /etc/docker/daemon.json
{
  "insecure-registries": ["registry.local:5000"]
}
```

## Caching

Docksmith caches registry responses to:
- Reduce API calls
- Respect rate limits
- Speed up checks

### Cache Settings

```yaml
environment:
  - CACHE_TTL=1h  # How long to cache responses
```

### Clear Cache

Trigger a fresh check that clears cache:

```bash
curl http://localhost:3000/api/check
```

Or use the trigger-check endpoint (uses cache):
```bash
curl -X POST http://localhost:3000/api/trigger-check
```

## LinuxServer Images

LinuxServer images on GHCR are fully supported:

```yaml
services:
  plex:
    image: ghcr.io/linuxserver/plex:latest
    labels:
      - docksmith.allow-latest=true

  sonarr:
    image: ghcr.io/linuxserver/sonarr:latest
    labels:
      - docksmith.allow-latest=true
```

Their build number suffixes (e.g., `1.0.0-ls123`) are normalized for comparison.

## Multi-Architecture Images

Docksmith handles multi-arch images automatically. It checks digests to detect updates even when tags don't change.

## Troubleshooting

### "Unauthorized" Errors

1. Check your credentials:
```bash
docker login registry.example.com
```

2. Verify the config is mounted:
```bash
docker exec docksmith cat /root/.docker/config.json
```

3. Check token hasn't expired (GHCR tokens can expire)

### Rate Limit Exceeded

Docker Hub rate limit. Solutions:
- Mount authenticated docker config
- Increase `CACHE_TTL` to reduce checks
- Use a pull-through cache

### "Connection Refused" on Private Registry

1. Check registry is accessible from Docksmith container:
```bash
docker exec docksmith curl https://registry.example.com/v2/
```

2. Check DNS resolution:
```bash
docker exec docksmith nslookup registry.example.com
```

3. For internal registries, ensure Docksmith is on the right network

### GHCR Returns 404

- Image may not exist or be private
- Check token has `read:packages` scope
- Token may have expired
- Verify docker config is mounted

### Tags Not Updating

1. Clear cache:
```bash
curl http://localhost:3000/api/check
```

2. Check cache TTL isn't too long

3. Verify the registry has new tags:
```bash
curl http://localhost:3000/api/registry/tags/nginx
```
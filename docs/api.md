# API Reference

Docksmith exposes a REST API at `/api/`.

## Contents

- [Endpoints](#endpoints)
- [Common Endpoints](#common-endpoints)
- [Error Responses](#error-responses)
- [Authentication](#authentication)

## Endpoints

### Health & Status

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | Server health check |
| GET | `/api/status` | System status with last check time |
| GET | `/api/docker-config` | Docker configuration info |

### Discovery & Checking

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/check` | Check all containers (clears cache) |
| POST | `/api/trigger-check` | Background check (uses cache) |
| GET | `/api/container/{name}/recheck` | Recheck single container |

### Updates

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/update` | Update single container |
| POST | `/api/update/batch` | Batch update multiple containers |
| POST | `/api/rollback` | Rollback to previous version |

### History & Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/operations` | List operations with filtering |
| GET | `/api/operations/{id}` | Get operation by ID |
| GET | `/api/history` | Check and update history |
| GET | `/api/policies` | Get rollback policies |

### Restart

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/restart` | Restart container (name in body) |
| POST | `/api/restart/container/{name}` | Restart container by name |
| POST | `/api/restart/stack/{name}` | Restart entire stack |
| POST | `/api/restart/start/{name}` | SSE-based restart with progress |

### Labels

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/labels/{container}` | Get container labels |
| POST | `/api/labels/set` | Set labels (restarts container) |
| POST | `/api/labels/remove` | Remove labels (restarts container) |

### Scripts

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/scripts` | List available scripts |
| GET | `/api/scripts/assigned` | List script assignments |
| POST | `/api/scripts/assign` | Assign script to container |
| DELETE | `/api/scripts/assign/{container}` | Remove assignment |

### Registry

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/registry/tags/{image}` | Get tags for image |

### Events

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/events` | SSE stream for real-time updates |

### Explorer

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/explorer` | Get all Docker resources (containers, images, networks, volumes) |
| GET | `/api/images` | List all images |
| GET | `/api/networks` | List all networks |
| GET | `/api/volumes` | List all volumes |
| DELETE | `/api/images/{id}` | Remove an image |
| DELETE | `/api/networks/{id}` | Remove a network |
| DELETE | `/api/volumes/{name}` | Remove a volume |

### Compose Mismatch

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/fix-compose-mismatch/{name}` | Fix container where running image differs from compose file |

### Container Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/containers/{name}/logs` | Get container logs |
| GET | `/api/containers/{name}/inspect` | Inspect container details |
| GET | `/api/containers/{name}/stats` | Get container resource stats |
| POST | `/api/containers/{name}/stop` | Stop a container |
| POST | `/api/containers/{name}/start` | Start a container |
| POST | `/api/containers/{name}/restart` | Restart a container |
| DELETE | `/api/containers/{name}` | Remove a container |

### Prune

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/prune/containers` | Remove stopped containers |
| POST | `/api/prune/images` | Remove unused images |
| POST | `/api/prune/networks` | Remove unused networks |
| POST | `/api/prune/volumes` | Remove unused volumes |
| POST | `/api/prune/system` | Remove all unused resources |

---

## Common Endpoints

### GET /api/health

Returns server health status.

```bash
curl http://localhost:3000/api/health
```

Response:
```json
{
  "status": "ok",
  "version": "1.0.0"
}
```

### GET /api/status

Returns system status including last check time. Used by Homepage widget.

```bash
curl http://localhost:3000/api/status
```

Response:
```json
{
  "data": {
    "total_checked": 25,
    "updates_found": 3,
    "last_cache_refresh": "2024-01-15T10:30:00Z",
    "last_background_run": "2024-01-15T10:30:00Z"
  }
}
```

### GET /api/check

Discovers containers and checks for updates. Clears registry cache.

```bash
curl http://localhost:3000/api/check
```

Response:
```json
{
  "data": {
    "containers": [
      {
        "name": "nginx",
        "stack": "web",
        "image": "nginx:1.24.0",
        "current_version": "1.24.0",
        "latest_version": "1.25.3",
        "status": "UPDATE_AVAILABLE",
        "update_available": true,
        "labels": {
          "docksmith.version-pin-major": "false"
        }
      }
    ],
    "total": 25,
    "updates_available": 3
  }
}
```

#### Container Status Values

| Status | Description |
|--------|-------------|
| `UP_TO_DATE` | Container is running the latest version |
| `UP_TO_DATE_PINNABLE` | Container uses `latest` tag, can be pinned to specific version |
| `UPDATE_AVAILABLE` | Newer version available |
| `UPDATE_AVAILABLE_BLOCKED` | Update available but blocked by pre-update check |
| `COMPOSE_MISMATCH` | Running image differs from compose file specification |
| `LOCAL_IMAGE` | Container uses locally built image (no registry) |
| `IGNORED` | Container is ignored via `docksmith.ignore` label |
| `ERROR` | Error checking container status |

#### Compose Mismatch Details

When a container has `status: "COMPOSE_MISMATCH"`, the response includes additional fields:

```json
{
  "name": "myapp",
  "status": "COMPOSE_MISMATCH",
  "image": "nginx:1.24.0",
  "compose_image": "nginx:1.25.0",
  "error": "Running image (nginx:1.24.0) differs from compose specification (nginx:1.25.0)"
}
```

- `image` — The image currently running
- `compose_image` — The image specified in the compose file
- `error` — Human-readable description of the mismatch

Use `POST /api/fix-compose-mismatch/{name}` to sync the container to the compose file specification.

### GET /api/container/{name}/recheck

Recheck a single container for updates. Useful after changing labels.

```bash
curl http://localhost:3000/api/container/nginx/recheck
```

Response:
```json
{
  "data": {
    "name": "nginx",
    "current_version": "1.24.0",
    "latest_version": "1.25.3",
    "update_available": true
  }
}
```

### POST /api/update

Update a single container.

```bash
curl -X POST http://localhost:3000/api/update \
  -H "Content-Type: application/json" \
  -d '{"container":"nginx"}'
```

With specific version:
```bash
curl -X POST http://localhost:3000/api/update \
  -H "Content-Type: application/json" \
  -d '{"container":"nginx","version":"1.25.0"}'
```

With script override:
```bash
curl -X POST http://localhost:3000/api/update \
  -H "Content-Type: application/json" \
  -d '{"container":"nginx","script":"/scripts/check.sh"}'
```

Response:
```json
{
  "data": {
    "operation_id": "op_2024011510302345",
    "container": "nginx",
    "status": "started"
  }
}
```

### POST /api/update/batch

Update multiple containers.

```bash
curl -X POST http://localhost:3000/api/update/batch \
  -H "Content-Type: application/json" \
  -d '{"containers":["nginx","redis","postgres"]}'
```

### POST /api/rollback

Rollback a previous update.

```bash
curl -X POST http://localhost:3000/api/rollback \
  -H "Content-Type: application/json" \
  -d '{"operation_id":"op_2024011510302345"}'
```

### POST /api/fix-compose-mismatch/{name}

Fix a container where the running image doesn't match the compose file specification. This can happen when:
- The compose file was edited but the container wasn't recreated
- The container lost its tag reference and is running with a bare SHA digest

The operation will:
1. Pull the image specified in the compose file
2. Recreate the container with `docker compose up -d`

```bash
curl -X POST http://localhost:3000/api/fix-compose-mismatch/mycontainer
```

Response:
```json
{
  "success": true,
  "data": {
    "operation_id": "op_2024011510302345"
  }
}
```

Error response (e.g., container uses `build:` instead of `image:`):
```json
{
  "success": false,
  "error": "failed to extract image from compose file: no image key found for service mycontainer"
}
```

**Note:** This endpoint only works for containers that use `image:` in their compose file. Containers using `build:` cannot be fixed this way - they need to be rebuilt manually.

### GET /api/operations

List operations with optional filtering.

```bash
# All operations
curl http://localhost:3000/api/operations

# Filter by container
curl "http://localhost:3000/api/operations?container=nginx"

# Filter by status
curl "http://localhost:3000/api/operations?status=complete"

# Filter by type
curl "http://localhost:3000/api/operations?type=update"

# Limit results
curl "http://localhost:3000/api/operations?limit=10"
```

Response:
```json
{
  "data": [
    {
      "operation_id": "op_2024011510302345",
      "container_name": "nginx",
      "stack_name": "web",
      "operation_type": "update",
      "status": "complete",
      "old_version": "1.24.0",
      "new_version": "1.25.3",
      "started_at": "2024-01-15T10:30:23Z",
      "completed_at": "2024-01-15T10:31:45Z"
    }
  ]
}
```

### GET /api/policies

Get rollback policies for containers.

```bash
curl http://localhost:3000/api/policies
```

Response:
```json
{
  "data": {
    "containers": {
      "nginx": {
        "can_rollback": true,
        "last_operation_id": "op_2024011510302345",
        "old_version": "1.24.0"
      }
    }
  }
}
```

### POST /api/labels/set

Set labels on a container. Updates compose file and restarts container.

```bash
curl -X POST http://localhost:3000/api/labels/set \
  -H "Content-Type: application/json" \
  -d '{
    "container": "nginx",
    "labels": {
      "docksmith.version-pin-major": "true",
      "docksmith.ignore": "false"
    }
  }'
```

### POST /api/labels/remove

Remove labels from a container.

```bash
curl -X POST http://localhost:3000/api/labels/remove \
  -H "Content-Type: application/json" \
  -d '{
    "container": "nginx",
    "labels": ["docksmith.ignore"]
  }'
```

### GET /api/events

Server-Sent Events stream for real-time updates.

```bash
curl -N http://localhost:3000/api/events
```

Event types:
- `update.progress` — Update stage changes
- `container.updated` — Update completed
- `check.progress` — Background check progress
- `restart.progress` — Restart operation progress
- `container.stopped` — Container stopped
- `container.removed` — Container removed

Event format:
```
data: {"type":"update.progress","payload":{"container":"nginx","stage":"pulling_image","progress":50,"message":"Pulling nginx:1.25.3"}}
```

### GET /api/registry/tags/{image}

Get available tags for an image. Useful for testing regex patterns.

```bash
curl http://localhost:3000/api/registry/tags/nginx
curl http://localhost:3000/api/registry/tags/ghcr.io/linuxserver/plex
```

Response:
```json
{
  "data": {
    "image_ref": "nginx",
    "tags": ["1.25.3", "1.25.2", "1.25.1", "1.24.0", "latest", "alpine"],
    "count": 6
  }
}
```

### GET /api/explorer

Get all Docker resources in one call.

```bash
curl http://localhost:3000/api/explorer
```

Response:
```json
{
  "data": {
    "containers": [...],
    "images": [...],
    "networks": [...],
    "volumes": [...]
  }
}
```

### GET /api/containers/{name}/logs

Get container logs.

```bash
# Get last 100 lines
curl "http://localhost:3000/api/containers/nginx/logs?tail=100"

# Get logs since timestamp
curl "http://localhost:3000/api/containers/nginx/logs?since=2024-01-15T10:00:00Z"
```

Response:
```json
{
  "data": {
    "logs": "2024-01-15 10:30:00 [notice] nginx started..."
  }
}
```

### GET /api/containers/{name}/stats

Get container resource statistics.

```bash
curl http://localhost:3000/api/containers/nginx/stats
```

Response:
```json
{
  "data": {
    "cpu_percent": 2.5,
    "memory_usage": 52428800,
    "memory_limit": 1073741824,
    "memory_percent": 4.88,
    "network_rx": 1048576,
    "network_tx": 524288
  }
}
```

### POST /api/containers/{name}/stop

Stop a running container.

```bash
curl -X POST http://localhost:3000/api/containers/nginx/stop
```

### POST /api/containers/{name}/start

Start a stopped container.

```bash
curl -X POST http://localhost:3000/api/containers/nginx/start
```

### DELETE /api/containers/{name}

Remove a container.

```bash
# Remove stopped container
curl -X DELETE http://localhost:3000/api/containers/nginx

# Force remove running container
curl -X DELETE "http://localhost:3000/api/containers/nginx?force=true"
```

### POST /api/prune/system

Remove all unused Docker resources.

```bash
curl -X POST http://localhost:3000/api/prune/system
```

Response:
```json
{
  "data": {
    "containers_deleted": 3,
    "images_deleted": 5,
    "networks_deleted": 2,
    "volumes_deleted": 1,
    "space_reclaimed": 1073741824
  }
}
```

---

## Error Responses

All endpoints return errors in a consistent format:

```json
{
  "error": {
    "message": "Container not found: nginx",
    "code": "NOT_FOUND"
  }
}
```

HTTP status codes:
- `200` — Success
- `400` — Bad request (invalid parameters)
- `404` — Not found
- `429` — Rate limited
- `500` — Server error

---

## Authentication

Docksmith has no built-in authentication. Deploy behind an authenticating reverse proxy for production use.

See [integrations.md](integrations.md) for reverse proxy examples.

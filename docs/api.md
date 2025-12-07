# API Reference

Docksmith exposes a REST API at `/api/`.

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

### Updates

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/update` | Update single container |
| POST | `/api/update/batch` | Batch update multiple containers |
| POST | `/api/rollback` | Rollback to previous version |

### History

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/operations` | List operations with filtering |
| GET | `/api/operations/{id}` | Get operation by ID |
| GET | `/api/history` | Check and update history |
| GET | `/api/backups` | List available backups |
| GET | `/api/policies` | Rollback policies |

### Restart

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/restart/container/{name}` | Restart container |
| POST | `/api/restart/stack/{name}` | Restart stack |
| POST | `/api/restart/start/{name}` | SSE-based restart |

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

---

## Common Endpoints

### GET /api/health

Returns server health status.

```bash
curl http://localhost:8080/api/health
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
curl http://localhost:8080/api/status
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
curl http://localhost:8080/api/check
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

### POST /api/update

Update a single container.

```bash
curl -X POST http://localhost:8080/api/update \
  -H "Content-Type: application/json" \
  -d '{"container":"nginx"}'
```

With specific version:
```bash
curl -X POST http://localhost:8080/api/update \
  -H "Content-Type: application/json" \
  -d '{"container":"nginx","version":"1.25.0"}'
```

With script override:
```bash
curl -X POST http://localhost:8080/api/update \
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
curl -X POST http://localhost:8080/api/update/batch \
  -H "Content-Type: application/json" \
  -d '{"containers":["nginx","redis","postgres"]}'
```

### POST /api/rollback

Rollback a previous update.

```bash
curl -X POST http://localhost:8080/api/rollback \
  -H "Content-Type: application/json" \
  -d '{"operation_id":"op_2024011510302345"}'
```

### GET /api/operations

List operations with optional filtering.

```bash
# All operations
curl http://localhost:8080/api/operations

# Filter by container
curl "http://localhost:8080/api/operations?container=nginx"

# Filter by status
curl "http://localhost:8080/api/operations?status=complete"

# Limit results
curl "http://localhost:8080/api/operations?limit=10"
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

### POST /api/labels/set

Set labels on a container. Updates compose file and restarts container.

```bash
curl -X POST http://localhost:8080/api/labels/set \
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
curl -X POST http://localhost:8080/api/labels/remove \
  -H "Content-Type: application/json" \
  -d '{
    "container": "nginx",
    "labels": ["docksmith.ignore"]
  }'
```

### GET /api/events

Server-Sent Events stream for real-time updates.

```bash
curl -N http://localhost:8080/api/events
```

Event types:
- `update.progress` — Update stage changes
- `container.updated` — Update completed
- `check.progress` — Background check progress

Event format:
```
data: {"type":"update.progress","payload":{"container":"nginx","stage":"pulling_image","progress":50,"message":"Pulling nginx:1.25.3"}}
```

### GET /api/registry/tags/{image}

Get available tags for an image. Useful for testing regex patterns.

```bash
curl http://localhost:8080/api/registry/tags/nginx
curl http://localhost:8080/api/registry/tags/ghcr.io/linuxserver/plex
```

Response:
```json
{
  "data": {
    "tags": ["1.25.3", "1.25.2", "1.25.1", "1.24.0", "latest", "alpine"]
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
- `500` — Server error

---

## Authentication

Docksmith has no built-in authentication. Deploy behind an authenticating reverse proxy for production use.

See [integrations.md](integrations.md) for reverse proxy examples.

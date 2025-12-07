# Integrations

Docksmith integrates with common self-hosted tools.

## Homepage Dashboard

Add a Docksmith widget to [gethomepage](https://gethomepage.dev) using the customapi widget type.

### Basic Setup

```yaml
# services.yaml
- Services:
  - Docksmith:
      icon: docker.png
      href: https://docksmith.example.com
      description: Container update management
      server: docker
      container: docksmith
      widget:
        type: customapi
        url: http://docksmith:8080/api/status
        mappings:
          - field: data.total_checked
            label: Monitored
            format: number
          - field: data.updates_found
            label: Updates
            format: number
          - field: data.last_cache_refresh
            label: Last Check
            format: relativeDate
```

### With Update Count Highlighting

Color the update count red when updates are available:

```yaml
widget:
  type: customapi
  url: http://docksmith:8080/api/status
  mappings:
    - field: data.total_checked
      label: Monitored
      format: number
    - field: data.updates_found
      label: Updates
      format: number
      color: "{{if data.updates_found > 0}}red{{else}}green{{end}}"
    - field: data.last_cache_refresh
      label: Last Check
      format: relativeDate
```

### API Status Response

The `/api/status` endpoint returns:

```json
{
  "data": {
    "total_checked": 25,
    "updates_found": 3,
    "last_cache_refresh": "2024-01-15T10:30:00Z"
  }
}
```

## Traefik

Route traffic to Docksmith with automatic HTTPS:

```yaml
services:
  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    container_name: docksmith
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - /home/user/stacks:/stacks:rw
    command: ["api", "--port", "8080"]
    labels:
      - traefik.enable=true
      - traefik.http.routers.docksmith.rule=Host(`docksmith.example.com`)
      - traefik.http.routers.docksmith.entrypoints=websecure
      - traefik.http.routers.docksmith.tls.certresolver=letsencrypt
      - traefik.http.services.docksmith.loadbalancer.server.port=8080
      # Prevent Docksmith from updating itself
      - docksmith.ignore=true
    networks:
      - traefik

networks:
  traefik:
    external: true
```

## Caddy

Add to your Caddyfile:

```
docksmith.example.com {
    reverse_proxy docksmith:8080
}
```

Or with docker-compose:

```yaml
services:
  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    container_name: docksmith
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - /home/user/stacks:/stacks:rw
    command: ["api", "--port", "8080"]
    networks:
      - caddy

networks:
  caddy:
    external: true
```

## Tailscale

Deploy Docksmith on your Tailscale network for secure remote access without exposing ports to the internet.

### Sidecar Container

```yaml
services:
  tailscale:
    image: tailscale/tailscale:latest
    container_name: docksmith-ts
    hostname: docksmith
    cap_add:
      - NET_ADMIN
      - SYS_MODULE
    environment:
      - TS_AUTHKEY=tskey-auth-xxxxx
      - TS_STATE_DIR=/var/lib/tailscale
    volumes:
      - ./tailscale:/var/lib/tailscale
      - /dev/net/tun:/dev/net/tun

  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    container_name: docksmith
    network_mode: service:tailscale
    depends_on:
      - tailscale
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - /home/user/stacks:/stacks:rw
    command: ["api", "--port", "8080"]
```

Access at `http://docksmith:8080` from any device on your Tailnet.

### With Tailscale Serve

Expose via HTTPS using Tailscale's built-in certificates:

```bash
# On the Tailscale container or host
tailscale serve --bg 8080
```

## Authelia / Authentik

Docksmith has no built-in authentication. Use an authenticating reverse proxy.

### Authelia with Traefik

```yaml
labels:
  - traefik.http.routers.docksmith.middlewares=authelia@docker
```

### Authentik with Traefik

```yaml
labels:
  - traefik.http.routers.docksmith.middlewares=authentik@docker
```

## Nginx Proxy Manager

1. Add a new proxy host
2. Domain: `docksmith.example.com`
3. Forward hostname: `docksmith`
4. Forward port: `8080`
5. Enable SSL with Let's Encrypt

## Uptime Monitoring

### Uptime Kuma

Add an HTTP monitor:
- URL: `http://docksmith:8080/api/health`
- Expected status: `200`

### Gatus

```yaml
endpoints:
  - name: Docksmith
    url: http://docksmith:8080/api/health
    interval: 5m
    conditions:
      - "[STATUS] == 200"
```

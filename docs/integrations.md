# Integrations

Docksmith integrates with common self-hosted tools.

## Contents

- [Homepage Dashboard](#homepage-dashboard)
- [Traefik](#traefik)
- [Tailscale](#tailscale)
- [Nginx Proxy Manager](#nginx-proxy-manager)
- [Uptime Monitoring](#uptime-monitoring)

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
        url: http://docksmith:3000/api/status
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
    # Server runs on port 3000 by default
    labels:
      - traefik.enable=true
      - traefik.http.routers.docksmith.rule=Host(`docksmith.example.com`)
      - traefik.http.routers.docksmith.entrypoints=websecure
      - traefik.http.routers.docksmith.tls.certresolver=letsencrypt
      - traefik.http.services.docksmith.loadbalancer.server.port=3000
      # Prevent Docksmith from updating itself
      - docksmith.ignore=true
    networks:
      - traefik

networks:
  traefik:
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
    # Server runs on port 3000 by default
```

Access at `http://docksmith:3000` from any device on your Tailnet.

### With Tailscale Serve

Expose via HTTPS using Tailscale's built-in certificates:

```bash
# On the Tailscale container or host
tailscale serve --bg 3000
```

## Nginx Proxy Manager

1. Add a new proxy host
2. Domain: `docksmith.example.com`
3. Forward hostname: `docksmith`
4. Forward port: `3000`
5. Enable SSL with Let's Encrypt

## Uptime Monitoring

### Uptime Kuma

Add an HTTP monitor:
- URL: `http://docksmith:3000/api/health`
- Expected status: `200`

### Gatus

```yaml
endpoints:
  - name: Docksmith
    url: http://docksmith:3000/api/health
    interval: 5m
    conditions:
      - "[STATUS] == 200"
```
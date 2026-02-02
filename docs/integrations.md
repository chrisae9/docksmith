# Integrations

Docksmith integrates with common self-hosted tools.

## Contents

- [Homepage Dashboard](#homepage-dashboard)
- [Tailscale + Traefik](#tailscale--traefik)
- [Tailscale Only](#tailscale-only)

## Homepage Dashboard

Add a Docksmith widget to [gethomepage](https://gethomepage.dev) using the customapi widget type.

### Basic Setup

```yaml
# services.yaml
- Services:
  - Docksmith:
      icon: docker.png
      href: https://docksmith.ts.example.com
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

## Tailscale + Traefik

> **Warning**: Docksmith has no built-in authentication. Do not expose it to the public internet. The configuration below is designed for local access only via Tailscale.

This setup uses Tailscale for secure network access and Traefik for HTTPS routing within your Tailnet.

### Architecture

```
Tailnet Device → Tailscale DNS (docksmith.ts.example.com) → Traefik → Docksmith
```

### Docker Compose

```yaml
services:
  # Tailscale sidecar - provides network access
  tailscale:
    image: tailscale/tailscale:latest
    container_name: docksmith-ts
    hostname: docksmith-ts
    cap_add:
      - NET_ADMIN
      - SYS_MODULE
    environment:
      - TS_AUTHKEY=tskey-auth-xxxxx  # Generate at https://login.tailscale.com/admin/settings/keys
      - TS_STATE_DIR=/var/lib/tailscale
      - TS_EXTRA_ARGS=--accept-routes
    volumes:
      - ./tailscale:/var/lib/tailscale
      - /dev/net/tun:/dev/net/tun
    restart: unless-stopped

  # Traefik for HTTPS (runs on Tailscale network)
  traefik:
    image: traefik:latest
    container_name: docksmith-traefik
    network_mode: service:tailscale
    depends_on:
      - tailscale
    command:
      - --api.insecure=true
      - --providers.docker=true
      - --providers.docker.exposedbydefault=false
      - --entrypoints.websecure.address=:443
      - --certificatesresolvers.tailscale.tailscale=true
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    restart: unless-stopped
    labels:
      - docksmith.ignore=true

  # Docksmith
  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    container_name: docksmith
    depends_on:
      - traefik
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - /home/user/stacks:/stacks:rw
    labels:
      - traefik.enable=true
      - traefik.http.routers.docksmith.rule=Host(`docksmith.ts.example.com`)
      - traefik.http.routers.docksmith.entrypoints=websecure
      - traefik.http.routers.docksmith.tls.certresolver=tailscale
      - traefik.http.services.docksmith.loadbalancer.server.port=3000
      - docksmith.ignore=true
    restart: unless-stopped
```

### Tailscale DNS Setup

1. Go to [Tailscale Admin Console](https://login.tailscale.com/admin/dns)
2. Enable MagicDNS if not already enabled
3. Add a DNS name for your Tailnet (e.g., `ts.example.com`)
4. The container will be accessible at `docksmith.ts.example.com`

### Notes

- **`docksmith.ignore=true`**: Prevents Docksmith from trying to update itself or Traefik
- **Tailscale certificates**: Traefik automatically obtains certificates from Tailscale for HTTPS
- **No public exposure**: Traffic never leaves your Tailnet

## Tailscale Only

For simpler setups without Traefik, use Tailscale directly:

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
    restart: unless-stopped

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
    labels:
      - docksmith.ignore=true
    restart: unless-stopped
```

Access at `http://docksmith:3000` from any device on your Tailnet.

### With Tailscale Serve (HTTPS)

For HTTPS without Traefik, use Tailscale's built-in serve feature:

```bash
# On the Tailscale container
tailscale serve --bg 3000
```

This provides HTTPS at `https://docksmith.ts.example.com` using Tailscale's certificates.

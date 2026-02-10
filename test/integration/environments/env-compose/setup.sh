#!/bin/bash
# Setup script for env-compose test environment
# Creates a compose file with env var references and a .env file
# Tests the .env file update functionality

set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Setting up env-compose test environment..."

# Create .env file
cat > "$DIR/.env" << 'EOF'
NGINX_IMAGE=nginx:1.24.0
REDIS_TAG=7.0.0
EOF

# Create backup
cp "$DIR/.env" "$DIR/.env.backup"

# Create docker-compose.yml
cat > "$DIR/docker-compose.yml" << 'EOF'
# Env-controlled compose environment for testing .env file updates
# NGINX_IMAGE is a full image ref, REDIS_TAG is a bare tag

services:
  nginx-env:
    image: ${NGINX_IMAGE:-nginx:1.24.0}
    container_name: test-nginx-env
    restart: unless-stopped
    ports:
      - "8095:80"
  redis-env:
    image: redis:${REDIS_TAG:-7.0.0}
    container_name: test-redis-env
    restart: unless-stopped
    ports:
      - "6385:6379"
EOF

# Create backup
cp "$DIR/docker-compose.yml" "$DIR/docker-compose.yml.backup"

echo "env-compose environment created:"
echo "  .env: NGINX_IMAGE=nginx:1.24.0, REDIS_TAG=7.0.0"
echo "  docker-compose.yml: uses \${NGINX_IMAGE} and \${REDIS_TAG}"

#!/bin/bash
# Teardown script for compose mismatch test environment

set -e

CONTAINER_NAME="test-nginx-mismatch"
HOST_COMPOSE_DIR="/home/chis/www/docksmith/test/integration/environments/compose-mismatch"

echo "Tearing down compose mismatch test environment..."

# Stop and remove the container
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

# Reset compose file to original state (with matching image)
docker run --rm -v "$HOST_COMPOSE_DIR:/compose" alpine:latest sh -c "cat > /compose/docker-compose.yml << 'COMPOSE_EOF'
# Compose mismatch test environment
# Run setup.sh to create a mismatch state for testing
services:
  nginx-mismatch:
    image: nginx:1.24.0
    container_name: test-nginx-mismatch
    restart: unless-stopped
    ports:
      - \"8095:80\"
COMPOSE_EOF
"

echo "Teardown complete."

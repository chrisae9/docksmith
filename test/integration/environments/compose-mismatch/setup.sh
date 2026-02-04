#!/bin/bash
# Setup script for compose mismatch test environment
# Creates a container that runs a different image than what's in the compose file
# This script can be run from inside the playwright container (uses docker commands directly)

set -e

CONTAINER_NAME="test-nginx-mismatch"
RUNNING_IMAGE="nginx:1.24.0"
COMPOSE_IMAGE="nginx:1.25.0"
# Host path for the compose file (must be accessible to Docksmith container)
HOST_COMPOSE_DIR="/home/chis/www/docksmith/test/integration/environments/compose-mismatch"
HOST_COMPOSE_FILE="$HOST_COMPOSE_DIR/docker-compose.yml"

echo "Setting up compose mismatch test environment..."

# Step 1: Remove any existing container
echo "Cleaning up any existing container..."
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

# Step 2: Pull the running image
echo "Pulling $RUNNING_IMAGE..."
docker pull "$RUNNING_IMAGE"

# Step 3: Create the compose file with the DIFFERENT image (creates the mismatch)
echo "Creating compose file with $COMPOSE_IMAGE..."

# We need to update the compose file on the HOST filesystem
# Since we're running in docker with the socket mounted, use a helper container
docker run --rm -v "$HOST_COMPOSE_DIR:/compose" alpine:latest sh -c "cat > /compose/docker-compose.yml << COMPOSE_EOF
# Compose mismatch test environment
# Container runs nginx:1.24.0 but compose specifies nginx:1.25.0 - THIS IS A MISMATCH
services:
  nginx-mismatch:
    image: nginx:1.25.0
    container_name: test-nginx-mismatch
    restart: unless-stopped
    ports:
      - \"8095:80\"
COMPOSE_EOF
"

# Step 4: Create and start the container with the OLDER image
echo "Starting container with $RUNNING_IMAGE (but compose says $COMPOSE_IMAGE)..."
docker run -d \
  --name "$CONTAINER_NAME" \
  --restart unless-stopped \
  -p 8095:80 \
  -l "com.docker.compose.project=compose-mismatch" \
  -l "com.docker.compose.service=nginx-mismatch" \
  -l "com.docker.compose.project.config_files=$HOST_COMPOSE_FILE" \
  -l "com.docker.compose.project.working_dir=$HOST_COMPOSE_DIR" \
  "$RUNNING_IMAGE"

# Step 5: Wait for container to be running
echo "Waiting for container to start..."
sleep 2

# Step 6: Verify container is running
if ! docker ps | grep -q "$CONTAINER_NAME"; then
    echo "ERROR: Container $CONTAINER_NAME is not running"
    exit 1
fi

echo ""
echo "Mismatch created successfully!"
echo "Container runs: $RUNNING_IMAGE"
echo "Compose file ($HOST_COMPOSE_FILE) says: $COMPOSE_IMAGE"
echo ""
echo "Container compose labels:"
docker inspect "$CONTAINER_NAME" --format '{{json .Config.Labels}}' | grep -o '"com.docker.compose[^"]*":"[^"]*"' | tr ',' '\n' || true

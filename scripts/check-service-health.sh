#!/bin/bash

# Pre-update check: Service Health
# Checks if service responds to health endpoint

set -e

# Configuration - customize these for your service
HEALTH_URL="${HEALTH_URL:-http://localhost:8080/health}"
TIMEOUT="${TIMEOUT:-5}"

echo "Checking service health for $CONTAINER_NAME"
echo "Health endpoint: $HEALTH_URL"

# Try to reach the health endpoint
if curl -sf --max-time "$TIMEOUT" "$HEALTH_URL" > /dev/null 2>&1; then
    echo "✓ Service is healthy and responding"
    exit 0
else
    echo "✗ Service is unhealthy or not responding"
    echo "Health check failed at $HEALTH_URL"
    exit 1
fi

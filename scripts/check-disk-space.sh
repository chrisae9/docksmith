#!/bin/bash

# Pre-update check: Disk Space
# Ensures disk usage is below threshold before updating

set -e

THRESHOLD=90
MOUNT_POINT="/"

echo "Checking disk space for $CONTAINER_NAME"
echo "Threshold: ${THRESHOLD}%"

# Get disk usage percentage
USAGE=$(df "$MOUNT_POINT" | tail -1 | awk '{print $5}' | sed 's/%//')

echo "Current usage: ${USAGE}%"

if [ "$USAGE" -lt "$THRESHOLD" ]; then
    echo "✓ Disk space OK: ${USAGE}% < ${THRESHOLD}%"
    exit 0
else
    echo "✗ Disk usage too high: ${USAGE}% >= ${THRESHOLD}%"
    echo "Please free up disk space before updating"
    exit 1
fi

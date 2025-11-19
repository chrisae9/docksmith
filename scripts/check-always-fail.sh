#!/bin/bash

# Pre-update check: Always Fail (Testing)
# This script always fails - useful for testing update blocking

echo "Running test check for $CONTAINER_NAME"
echo "This check always fails (for testing purposes)"
echo "✗ Check failed - update blocked"
echo "This is expected behavior for testing"

exit 1

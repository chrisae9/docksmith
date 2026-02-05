#!/bin/bash
# Wrapper script to run compose-mismatch tests with proper setup/teardown
# This runs on the HOST, not inside the playwright container

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MISMATCH_DIR="$SCRIPT_DIR/../integration/environments/compose-mismatch"

echo "=== Setting up compose mismatch test environment ==="
"$MISMATCH_DIR/setup.sh"

# Wait for Docksmith to detect the new container
echo ""
echo "=== Waiting for Docksmith to detect the mismatch ==="
sleep 5
curl -s -X POST https://docksmith.ts.chis.dev/api/trigger-check > /dev/null
sleep 3

echo ""
echo "=== Running Playwright tests ==="
cd "$SCRIPT_DIR"

# Run the tests, capturing exit code
set +e
npm run test -- specs/compose-mismatch.spec.ts "$@"
TEST_EXIT_CODE=$?
set -e

echo ""
echo "=== Tearing down compose mismatch test environment ==="
"$MISMATCH_DIR/teardown.sh"

exit $TEST_EXIT_CODE

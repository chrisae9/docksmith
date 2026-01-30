#!/bin/bash
# Run Playwright E2E tests with proper test environment setup
# This script sets up test containers, runs tests, and cleans up

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INTEGRATION_DIR="$SCRIPT_DIR/../integration"
RESET_SCRIPT="$INTEGRATION_DIR/scripts/reset.sh"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

print_info() { echo -e "${YELLOW}ℹ $1${NC}"; }
print_success() { echo -e "${GREEN}✓ $1${NC}"; }
print_error() { echo -e "${RED}✗ $1${NC}"; }

# Parse arguments
TEST_SUITE="${1:-all}"  # all, api, labels
EXTRA_ARGS="${@:2}"

# Determine which environments to set up
case "$TEST_SUITE" in
    api)
        ENVIRONMENTS=("basic-compose")
        SPEC_FILE="specs/api-endpoints.spec.ts"
        ;;
    labels)
        ENVIRONMENTS=("labels")
        SPEC_FILE="specs/labels.spec.ts"
        ;;
    all|*)
        ENVIRONMENTS=("basic-compose" "labels")
        SPEC_FILE=""
        ;;
esac

# Setup function
setup_environments() {
    print_info "Setting up test environments..."

    for env in "${ENVIRONMENTS[@]}"; do
        print_info "  Resetting $env..."
        "$RESET_SCRIPT" "$env" 2>&1 | grep -E "(Resetting|Starting|complete)" || true
    done

    # Wait for containers to be ready
    print_info "Waiting for test containers to be ready..."
    sleep 10

    # Trigger Docksmith to discover the new containers
    print_info "Triggering Docksmith discovery..."
    curl -s -X POST "${DOCKSMITH_URL:-http://localhost:8080}/api/trigger-check" > /dev/null
    sleep 5

    print_success "Test environments ready"
}

# Cleanup function
cleanup_environments() {
    print_info "Cleaning up test environments..."

    for env in "${ENVIRONMENTS[@]}"; do
        local env_path="$INTEGRATION_DIR/environments/$env"
        if [ -d "$env_path" ]; then
            print_info "  Stopping $env..."
            (cd "$env_path" && docker compose down 2>/dev/null) || true
        fi
    done

    print_success "Cleanup complete"
}

# Trap for cleanup on exit
trap cleanup_environments EXIT

# Main
echo ""
echo "========================================="
echo "Playwright E2E Tests"
echo "========================================="
echo ""

# Setup
setup_environments

# Run tests
print_info "Running Playwright tests..."
echo ""

cd "$SCRIPT_DIR"

if [ -n "$SPEC_FILE" ]; then
    docker compose run --rm playwright npx playwright test "$SPEC_FILE" $EXTRA_ARGS
else
    docker compose run --rm playwright npx playwright test $EXTRA_ARGS
fi

TEST_EXIT_CODE=$?

echo ""
if [ $TEST_EXIT_CODE -eq 0 ]; then
    print_success "All tests passed!"
else
    print_error "Some tests failed (exit code: $TEST_EXIT_CODE)"
fi

exit $TEST_EXIT_CODE

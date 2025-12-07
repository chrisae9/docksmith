#!/bin/bash
#
# Docksmith Unified Test Runner
# Runs all test layers in order: unit tests, integration tests, (optionally) E2E tests
#
# Usage:
#   ./scripts/test-all.sh           # Run unit + integration tests
#   ./scripts/test-all.sh --all     # Run all tests including E2E
#   ./scripts/test-all.sh --unit    # Run only unit tests
#   ./scripts/test-all.sh --e2e     # Run E2E tests (Playwright)
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Parse arguments
RUN_UNIT=false
RUN_INTEGRATION=false
RUN_E2E=false
VERBOSE=""

for arg in "$@"; do
    case $arg in
        --all)
            RUN_UNIT=true
            RUN_INTEGRATION=true
            RUN_E2E=true
            ;;
        --unit)
            RUN_UNIT=true
            ;;
        --integration)
            RUN_INTEGRATION=true
            ;;
        --e2e)
            RUN_E2E=true
            ;;
        -v|--verbose)
            VERBOSE="-v"
            ;;
        --help|-h)
            echo "Docksmith Test Runner"
            echo ""
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --all          Run all tests (unit + integration + E2E)"
            echo "  --unit         Run Go unit tests only"
            echo "  --integration  Run shell integration tests only"
            echo "  --e2e          Run Playwright E2E tests only"
            echo "  -v, --verbose  Verbose output"
            echo "  -h, --help     Show this help message"
            echo ""
            echo "Default (no options): Run unit + integration tests"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $arg${NC}"
            exit 1
            ;;
    esac
done

# Default: run unit and integration tests
if [ "$RUN_UNIT" = false ] && [ "$RUN_INTEGRATION" = false ] && [ "$RUN_E2E" = false ]; then
    RUN_UNIT=true
    RUN_INTEGRATION=true
fi

# Track results
UNIT_RESULT=0
INTEGRATION_RESULT=0
E2E_RESULT=0

echo -e "${BLUE}=========================================${NC}"
echo -e "${BLUE}    Docksmith Test Suite${NC}"
echo -e "${BLUE}=========================================${NC}"
echo ""

# Run unit tests
if [ "$RUN_UNIT" = true ]; then
    echo -e "${YELLOW}Running Go Unit Tests...${NC}"
    echo ""

    cd "$PROJECT_ROOT"

    if go test ./internal/... $VERBOSE; then
        echo ""
        echo -e "${GREEN}✓ Unit tests passed${NC}"
    else
        UNIT_RESULT=1
        echo ""
        echo -e "${RED}✗ Unit tests failed${NC}"
    fi
    echo ""
fi

# Run integration tests
if [ "$RUN_INTEGRATION" = true ]; then
    echo -e "${YELLOW}Running Integration Tests...${NC}"
    echo ""

    cd "$PROJECT_ROOT/test/integration"

    # Check if Docksmith is running
    if ! docker ps | grep -q docksmith; then
        echo -e "${YELLOW}Warning: Docksmith container not running. Some tests may fail.${NC}"
    fi

    if [ -x "./scripts/run-tests.sh" ]; then
        if ./scripts/run-tests.sh; then
            echo ""
            echo -e "${GREEN}✓ Integration tests passed${NC}"
        else
            INTEGRATION_RESULT=1
            echo ""
            echo -e "${RED}✗ Integration tests failed${NC}"
        fi
    else
        echo -e "${YELLOW}Integration test runner not found or not executable${NC}"
        echo "Expected: test/integration/scripts/run-tests.sh"
    fi
    echo ""
fi

# Run E2E tests
if [ "$RUN_E2E" = true ]; then
    echo -e "${YELLOW}Running Playwright E2E Tests...${NC}"
    echo ""

    cd "$PROJECT_ROOT/test/playwright"

    if [ -f "package.json" ]; then
        # Check if dependencies are installed
        if [ ! -d "node_modules" ]; then
            echo "Installing dependencies..."
            npm install
            npx playwright install chromium
        fi

        if npm test; then
            echo ""
            echo -e "${GREEN}✓ E2E tests passed${NC}"
        else
            E2E_RESULT=1
            echo ""
            echo -e "${RED}✗ E2E tests failed${NC}"
        fi
    else
        echo -e "${YELLOW}Playwright tests not found${NC}"
        echo "Expected: test/playwright/package.json"
    fi
    echo ""
fi

# Summary
echo -e "${BLUE}=========================================${NC}"
echo -e "${BLUE}    Test Summary${NC}"
echo -e "${BLUE}=========================================${NC}"
echo ""

TOTAL_FAILED=0

if [ "$RUN_UNIT" = true ]; then
    if [ $UNIT_RESULT -eq 0 ]; then
        echo -e "${GREEN}✓ Unit Tests: PASSED${NC}"
    else
        echo -e "${RED}✗ Unit Tests: FAILED${NC}"
        TOTAL_FAILED=$((TOTAL_FAILED + 1))
    fi
fi

if [ "$RUN_INTEGRATION" = true ]; then
    if [ $INTEGRATION_RESULT -eq 0 ]; then
        echo -e "${GREEN}✓ Integration Tests: PASSED${NC}"
    else
        echo -e "${RED}✗ Integration Tests: FAILED${NC}"
        TOTAL_FAILED=$((TOTAL_FAILED + 1))
    fi
fi

if [ "$RUN_E2E" = true ]; then
    if [ $E2E_RESULT -eq 0 ]; then
        echo -e "${GREEN}✓ E2E Tests: PASSED${NC}"
    else
        echo -e "${RED}✗ E2E Tests: FAILED${NC}"
        TOTAL_FAILED=$((TOTAL_FAILED + 1))
    fi
fi

echo ""

if [ $TOTAL_FAILED -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}$TOTAL_FAILED test suite(s) failed${NC}"
    exit 1
fi

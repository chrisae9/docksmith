#!/bin/bash
# Shared test utilities for Docksmith integration tests

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Docksmith API base URL
API_BASE="${API_BASE:-http://localhost:8081/api}"

# Print colored output
print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

print_header() {
    echo ""
    echo "========================================="
    echo "$1"
    echo "========================================="
}

# Wait for container to be healthy or running
wait_for_container() {
    local container_name="$1"
    local timeout="${2:-60}"
    local start_time=$(date +%s)

    print_info "Waiting for container $container_name (timeout: ${timeout}s)..."

    while true; do
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))

        if [ $elapsed -ge $timeout ]; then
            print_error "Timeout waiting for container $container_name"
            return 1
        fi

        if docker ps --format '{{.Names}}' | grep -q "^${container_name}$"; then
            # Check health status
            local health=$(docker inspect --format='{{.State.Health.Status}}' "$container_name" 2>/dev/null || echo "none")

            if [ "$health" = "healthy" ] || [ "$health" = "none" ]; then
                print_success "Container $container_name is ready (health: $health)"
                return 0
            fi
        fi

        sleep 2
    done
}

# Get container version from image tag
get_container_version() {
    local container_name="$1"
    docker inspect --format='{{.Config.Image}}' "$container_name" 2>/dev/null | cut -d':' -f2
}

# Get container restart timestamp
get_restart_time() {
    local container_name="$1"
    docker inspect --format='{{.State.StartedAt}}' "$container_name" 2>/dev/null
}

# Make API request to Docksmith
curl_api() {
    local method="$1"
    local endpoint="$2"
    local body="$3"
    local url="${API_BASE}${endpoint}"

    if [ "$method" = "GET" ]; then
        curl -s -X GET "$url"
    elif [ "$method" = "POST" ]; then
        if [ -n "$body" ]; then
            curl -s -X POST -H "Content-Type: application/json" -d "$body" "$url"
        else
            curl -s -X POST "$url"
        fi
    elif [ "$method" = "DELETE" ]; then
        curl -s -X DELETE "$url"
    fi
}

# Assert that API response has success=true
assert_api_success() {
    local response="$1"
    local message="$2"

    TESTS_RUN=$((TESTS_RUN + 1))

    if echo "$response" | jq -e '.success == true' > /dev/null 2>&1; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "$message"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "$message"
        print_info "Response: $response"
        return 1
    fi
}

# Assert container has specific update status
assert_status() {
    local container_name="$1"
    local expected_status="$2"
    local message="${3:-Container $container_name should have status $expected_status}"

    TESTS_RUN=$((TESTS_RUN + 1))

    local response=$(curl_api GET "/status")
    local actual_status=$(echo "$response" | jq -r ".data.containers[] | select(.container_name == \"$container_name\") | .status")

    if [ "$actual_status" = "$expected_status" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "$message"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "$message (got: $actual_status)"
        return 1
    fi
}

# Assert container is running specific version
assert_version() {
    local container_name="$1"
    local expected_version="$2"
    local message="${3:-Container $container_name should be running version $expected_version}"

    TESTS_RUN=$((TESTS_RUN + 1))

    local actual_version=$(get_container_version "$container_name")

    if [ "$actual_version" = "$expected_version" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "$message"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "$message (got: $actual_version)"
        return 1
    fi
}

# Assert container exists in discovery results
assert_container_exists() {
    local container_name="$1"
    local message="${2:-Container $container_name should exist in discovery}"

    TESTS_RUN=$((TESTS_RUN + 1))

    local response=$(curl_api GET "/status")
    local found=$(echo "$response" | jq -r ".data.containers[] | select(.container_name == \"$container_name\") | .container_name")

    if [ "$found" = "$container_name" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "$message"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "$message"
        return 1
    fi
}

# Assert container does NOT exist in discovery results
assert_container_not_exists() {
    local container_name="$1"
    local message="${2:-Container $container_name should NOT exist in discovery}"

    TESTS_RUN=$((TESTS_RUN + 1))

    local response=$(curl_api GET "/status")
    local found=$(echo "$response" | jq -r ".data.containers[] | select(.container_name == \"$container_name\") | .container_name")

    if [ -z "$found" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "$message"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "$message"
        return 1
    fi
}

# Print test summary
print_test_summary() {
    print_header "TEST SUMMARY"
    echo "Total tests: $TESTS_RUN"
    echo -e "${GREEN}Passed: $TESTS_PASSED${NC}"
    echo -e "${RED}Failed: $TESTS_FAILED${NC}"

    if [ $TESTS_FAILED -eq 0 ]; then
        print_success "All tests passed!"
        return 0
    else
        print_error "Some tests failed"
        return 1
    fi
}

# Reset environment to old versions
reset_environment() {
    local env_name="$1"
    local env_path="/home/chis/www/docksmith/test/integration/environments/$env_name"

    print_info "Resetting environment: $env_name"

    if [ ! -d "$env_path" ]; then
        print_error "Environment not found: $env_path"
        return 1
    fi

    cd "$env_path" && docker compose down && docker compose up -d
    cd - > /dev/null
}

# Check if Docksmith is running
check_docksmith() {
    print_info "Checking if Docksmith is running..."

    if ! docker ps | grep -q docksmith; then
        print_error "Docksmith container is not running"
        return 1
    fi

    local health_response=$(curl_api GET "/health")
    if ! echo "$health_response" | jq -e '.success == true' > /dev/null 2>&1; then
        print_error "Docksmith API is not healthy"
        return 1
    fi

    print_success "Docksmith is running and healthy"
    return 0
}

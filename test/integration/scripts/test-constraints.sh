#!/bin/bash
# Test Docker constraint handling
# Tests health checks, depends_on, and restart policies
# This script is self-contained: it sets up, tests, and cleans up its own environment

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_DIR="$SCRIPT_DIR/../environments"
source "$SCRIPT_DIR/helpers.sh"

# Test configuration
TEST_STACK="constraints"
TEST_STACK2="include-compose"
ENV_PATH="$ENV_DIR/$TEST_STACK"
ENV_PATH2="$ENV_DIR/$TEST_STACK2"

# Setup function
setup() {
    print_header "Setting up constraint test environments"

    if [ ! -d "$ENV_PATH" ]; then
        print_error "Environment directory not found: $ENV_PATH"
        exit 1
    fi

    print_info "Resetting $TEST_STACK environment..."
    "$SCRIPT_DIR/reset.sh" "$TEST_STACK"
    sleep 5

    print_info "Resetting $TEST_STACK2 environment..."
    "$SCRIPT_DIR/reset.sh" "$TEST_STACK2"
    sleep 5
}

# Cleanup function
cleanup() {
    print_header "Cleaning up constraint test environments"

    if [ -d "$ENV_PATH" ]; then
        print_info "Stopping $TEST_STACK containers..."
        (cd "$ENV_PATH" && docker compose down 2>&1 | grep -E "(Stopping|Removing|Network)" || true)
    fi

    if [ -d "$ENV_PATH2" ]; then
        print_info "Stopping $TEST_STACK2 containers..."
        (cd "$ENV_PATH2" && docker compose down 2>&1 | grep -E "(Stopping|Removing|Network)" || true)
    fi
}

# Trap cleanup on exit
trap cleanup EXIT

print_header "Testing Docker Constraint Handling"

# Test 1: Health check monitoring
test_health_checks() {
    print_info "Test: Health check monitoring"

    local container="test-constraints-db"

    # Update container (has health check)
    print_info "Updating container with health check..."
    local body='{"container_name":"'"$container"'","target_version":"18.1"}'
    local response=$(curl_api POST "/update" "$body")
    assert_api_success "$response" "Update initiated for container with health check"

    # Wait for update to complete (health check should be monitored)
    sleep 30

    # Verify container is healthy
    local health=$(docker inspect --format='{{.State.Health.Status}}' "$container")

    TESTS_RUN=$((TESTS_RUN + 1))

    if [ "$health" = "healthy" ]; then
        print_success "Container is healthy after update"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Container health check failed: $health"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Verify version was updated
    assert_version "$container" "18.1" "Database updated to version 18.1"
}

# Test 2: depends_on chain ordering
test_depends_on() {
    print_info "Test: depends_on chain (db → api → web)"

    local db="test-constraints-db"
    local api="test-constraints-api"
    local web="test-constraints-web"

    # Get initial restart times
    local db_time_before=$(get_restart_time "$db")
    local api_time_before=$(get_restart_time "$api")
    local web_time_before=$(get_restart_time "$web")

    print_info "Initial restart times captured"

    # Update db container
    print_info "Updating database (should trigger dependent restarts)..."
    local body='{"container_name":"'"$db"'","target_version":"18.1"}'
    local response=$(curl_api POST "/update" "$body")
    assert_api_success "$response" "Database update initiated"

    # Wait for update and dependent restarts
    sleep 45

    # Get new restart times
    local db_time_after=$(get_restart_time "$db")
    local api_time_after=$(get_restart_time "$api")
    local web_time_after=$(get_restart_time "$web")

    # Verify all containers were restarted
    TESTS_RUN=$((TESTS_RUN + 3))

    if [ "$db_time_after" != "$db_time_before" ]; then
        print_success "Database was restarted"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Database was not restarted"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    if [ "$api_time_after" != "$api_time_before" ]; then
        print_success "API (depends on db) was restarted"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "API was not restarted"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    if [ "$web_time_after" != "$web_time_before" ]; then
        print_success "Web (depends on api) was restarted"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Web was not restarted"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Verify restart ordering (db < api < web)
    TESTS_RUN=$((TESTS_RUN + 1))

    # Convert timestamps to epoch seconds for comparison
    db_epoch=$(date -d "$db_time_after" +%s 2>/dev/null || echo "0")
    api_epoch=$(date -d "$api_time_after" +%s 2>/dev/null || echo "0")
    web_epoch=$(date -d "$web_time_after" +%s 2>/dev/null || echo "0")

    if [ "$db_epoch" -le "$api_epoch" ] && [ "$api_epoch" -le "$web_epoch" ]; then
        print_success "Restart ordering correct: db → api → web"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Restart ordering incorrect"
        print_info "  DB: $db_time_after ($db_epoch)"
        print_info "  API: $api_time_after ($api_epoch)"
        print_info "  Web: $web_time_after ($web_epoch)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# Test 3: Restart policy preservation
test_restart_policies() {
    print_info "Test: Restart policy preservation"

    local db="test-constraints-db"      # restart: unless-stopped
    local api="test-constraints-api"    # restart: always
    local web="test-constraints-web"    # restart: on-failure

    # Update all containers
    print_info "Updating containers to verify restart policies are preserved..."

    # Update db
    local body='{"container_name":"'"$db"'","target_version":"18.1"}'
    curl_api POST "/update" "$body" > /dev/null
    sleep 15

    # Update api
    local body='{"container_name":"'"$api"'","target_version":"1.29.3"}'
    curl_api POST "/update" "$body" > /dev/null
    sleep 15

    # Update web
    local body='{"container_name":"'"$web"'","target_version":"1.29.3"}'
    curl_api POST "/update" "$body" > /dev/null
    sleep 15

    # Verify restart policies
    TESTS_RUN=$((TESTS_RUN + 3))

    local db_policy=$(docker inspect --format='{{.HostConfig.RestartPolicy.Name}}' "$db")
    local api_policy=$(docker inspect --format='{{.HostConfig.RestartPolicy.Name}}' "$api")
    local web_policy=$(docker inspect --format='{{.HostConfig.RestartPolicy.Name}}' "$web")

    if [ "$db_policy" = "unless-stopped" ]; then
        print_success "DB restart policy preserved: $db_policy"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "DB restart policy changed: $db_policy (expected: unless-stopped)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    if [ "$api_policy" = "always" ]; then
        print_success "API restart policy preserved: $api_policy"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "API restart policy changed: $api_policy (expected: always)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    if [ "$web_policy" = "on-failure" ]; then
        print_success "Web restart policy preserved: $web_policy"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Web restart policy changed: $web_policy (expected: on-failure)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# Test 4: Include-based compose structure
test_include_compose() {
    print_info "Test: Include-based compose structure"

    # Use a container from include-compose environment
    local container="rooday-nginx"

    # Verify container is discovered despite multi-file structure
    assert_container_exists "$container" "Container from include-based compose discovered"

    # Update container
    print_info "Updating container from include-based compose..."
    local body='{"container_name":"'"$container"'","target_version":"1.29.3"}'
    local response=$(curl_api POST "/update" "$body")
    assert_api_success "$response" "Update works with include-based compose"

    sleep 15

    # Verify version
    assert_version "$container" "1.29.3" "Include-based compose container updated"
}

# Main test execution
main() {
    check_docksmith || exit 1

    # Setup environments
    setup

    print_info "Using environments: $TEST_STACK, $TEST_STACK2"
    echo ""

    # Run all constraint tests
    test_health_checks
    test_depends_on
    test_restart_policies
    test_include_compose

    # Print summary
    print_test_summary
}

main "$@"

#!/bin/bash
# Test all Docksmith API endpoints
# Tests read and write operations against basic-compose environment
# This script is self-contained: it sets up, tests, and cleans up its own environment

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_DIR="$SCRIPT_DIR/../environments"
source "$SCRIPT_DIR/helpers.sh"

# Test configuration
TEST_CONTAINER="test-nginx-basic"
TEST_STACK="basic-compose"
ENV_PATH="$ENV_DIR/$TEST_STACK"

# Setup function
setup() {
    print_header "Setting up API test environment"

    if [ ! -d "$ENV_PATH" ]; then
        print_error "Environment directory not found: $ENV_PATH"
        exit 1
    fi

    print_info "Resetting $TEST_STACK environment..."
    "$SCRIPT_DIR/reset.sh" "$TEST_STACK"
    sleep 5
}

# Cleanup function
cleanup() {
    print_header "Cleaning up API test environment"

    if [ -d "$ENV_PATH" ]; then
        print_info "Stopping $TEST_STACK containers..."
        (cd "$ENV_PATH" && docker compose down 2>&1 | grep -E "(Stopping|Removing|Network)" || true)
    fi
}

# Trap cleanup on exit
trap cleanup EXIT

print_header "Testing Docksmith API Endpoints"

# Track operation IDs for testing
UPDATE_OPERATION_ID=""
BATCH_OPERATION_ID=""
STOP_OPERATION_ID=""
REMOVE_OPERATION_ID=""

# Test 1: GET /api/health
test_health() {
    print_info "Test: GET /api/health"
    local response=$(curl_api GET "/health")
    assert_api_success "$response" "Health endpoint returns success"
}

# Test 2: GET /api/docker-config
test_docker_config() {
    print_info "Test: GET /api/docker-config"
    local response=$(curl_api GET "/docker-config")
    assert_api_success "$response" "Docker config endpoint returns success"
}

# Test 3: GET /api/status
test_status() {
    print_info "Test: GET /api/status"
    local response=$(curl_api GET "/status")
    assert_api_success "$response" "Status endpoint returns success"

    # Verify test container is in the list
    assert_container_exists "$TEST_CONTAINER" "Test container appears in status"
}

# Test 4: GET /api/check (triggers cache refresh)
test_check() {
    print_info "Test: GET /api/check"
    local response=$(curl_api GET "/check")
    assert_api_success "$response" "Check endpoint returns success"

    # Wait a moment for check to complete
    sleep 2

    # Verify update was detected
    assert_status "$TEST_CONTAINER" "UPDATE_AVAILABLE" "Update detected after check"
}

# Test 5: POST /api/trigger-check (background check)
test_trigger_check() {
    print_info "Test: POST /api/trigger-check"
    local response=$(curl_api POST "/trigger-check")
    assert_api_success "$response" "Trigger check endpoint returns success"
    sleep 2
}

# Test 6: POST /api/update (single container)
test_update_single() {
    print_info "Test: POST /api/update"
    local body='{"container_name":"'"$TEST_CONTAINER"'","target_version":"1.29.3"}'
    local response=$(curl_api POST "/update" "$body")
    assert_api_success "$response" "Single container update initiated"

    # Extract and save operation ID for rollback test
    UPDATE_OPERATION_ID=$(echo "$response" | jq -r '.data.operation_id')
    print_info "Operation ID: $UPDATE_OPERATION_ID"

    # Wait for operation to complete (condition-based, not fixed sleep)
    wait_for_operation "$UPDATE_OPERATION_ID" 120

    # Verify new version
    assert_version "$TEST_CONTAINER" "1.29.3" "Container updated to version 1.29.3"
}

# Test 7: GET /api/operations
test_operations() {
    print_info "Test: GET /api/operations"
    local response=$(curl_api GET "/operations?limit=10")
    assert_api_success "$response" "Operations endpoint returns success"

    # Verify we have operations
    local count=$(echo "$response" | jq -r '.data.count')
    if [ "$count" -gt 0 ]; then
        print_success "Found $count operations in history"
    else
        print_error "No operations found"
    fi
}

# Test 8: GET /api/operations/{id}
test_operation_by_id() {
    print_info "Test: GET /api/operations/{id}"

    # Get most recent operation ID
    local ops_response=$(curl_api GET "/operations?limit=1")
    local op_id=$(echo "$ops_response" | jq -r '.data.operations[0].operation_id')

    if [ -z "$op_id" ] || [ "$op_id" = "null" ]; then
        print_error "No operation ID found to test"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi

    local response=$(curl_api GET "/operations/$op_id")
    assert_api_success "$response" "Operation by ID endpoint returns success"
}

# Test 9: POST /api/rollback
test_rollback() {
    print_info "Test: POST /api/rollback"

    # Use the operation ID we saved from the update test
    if [ -z "$UPDATE_OPERATION_ID" ] || [ "$UPDATE_OPERATION_ID" = "null" ]; then
        print_error "No operation ID saved from update test"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        TESTS_RUN=$((TESTS_RUN + 1))
        return 1
    fi

    print_info "Rolling back operation: $UPDATE_OPERATION_ID"
    local body='{"operation_id":"'"$UPDATE_OPERATION_ID"'"}'
    local response=$(curl_api POST "/rollback" "$body")
    assert_api_success "$response" "Rollback initiated"

    # Extract rollback operation ID
    local rollback_op_id=$(echo "$response" | jq -r '.data.operation_id')

    # Wait for rollback to complete (condition-based, not fixed sleep)
    wait_for_operation "$rollback_op_id" 120

    # Verify rolled back to old version
    assert_version "$TEST_CONTAINER" "1.25.0" "Container rolled back to version 1.25.0"
}

# Test 10: GET /api/history
test_history() {
    print_info "Test: GET /api/history"
    local response=$(curl_api GET "/history?limit=10")
    assert_api_success "$response" "History endpoint returns success"

    local count=$(echo "$response" | jq -r '.data.count')
    if [ "$count" -gt 0 ]; then
        print_success "Found $count history entries"
    else
        print_error "No history entries found"
    fi
}

# Test 11: GET /api/labels/{container}
test_get_labels() {
    print_info "Test: GET /api/labels/{container}"
    local response=$(curl_api GET "/labels/$TEST_CONTAINER")
    assert_api_success "$response" "Get labels endpoint returns success"
}

# Test 12: POST /api/labels/set
test_set_labels() {
    print_info "Test: POST /api/labels/set"
    # Note: We don't use no_restart here so the container picks up the new labels
    local body='{"container":"'"$TEST_CONTAINER"'","ignore":true}'
    local response=$(curl_api POST "/labels/set" "$body")
    assert_api_success "$response" "Set labels endpoint returns success"

    # Wait for container restart and labels to be applied
    sleep 8

    # Verify label was set on the running container
    local labels_response=$(curl_api GET "/labels/$TEST_CONTAINER")
    local ignore_label=$(echo "$labels_response" | jq -r '.data.labels."docksmith.ignore"')

    if [ "$ignore_label" = "true" ]; then
        print_success "Label docksmith.ignore set successfully"
    else
        print_error "Label docksmith.ignore not set correctly"
    fi
}

# Test 13: DELETE /api/labels/remove (actually POST in implementation)
test_remove_labels() {
    print_info "Test: POST /api/labels/remove"
    local body='{"container":"'"$TEST_CONTAINER"'","label_names":["docksmith.ignore"]}'
    local response=$(curl_api POST "/labels/remove" "$body")
    assert_api_success "$response" "Remove labels endpoint returns success"

    sleep 8
}

# Test 14: POST /api/restart/container/{name}
test_restart_container() {
    print_info "Test: POST /api/restart/container/{name}"
    local old_restart_time=$(get_restart_time "$TEST_CONTAINER")

    local response=$(curl_api POST "/restart/container/$TEST_CONTAINER")
    assert_api_success "$response" "Restart container endpoint returns success"

    sleep 5

    local new_restart_time=$(get_restart_time "$TEST_CONTAINER")

    if [ "$new_restart_time" != "$old_restart_time" ]; then
        print_success "Container restarted successfully"
    else
        print_error "Container restart time unchanged"
    fi
}

# Test 15: POST /api/containers/{name}/stop
test_stop_container() {
    print_info "Test: POST /api/containers/{name}/stop"

    # Use Redis container for stop test (we'll start it again after)
    local container="test-redis-basic"

    # Verify container is running first
    local state=$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null || echo "false")
    if [ "$state" != "true" ]; then
        print_info "Container not running, starting it first..."
        docker start "$container" 2>/dev/null || true
        sleep 3
    fi

    local response=$(curl_api POST "/containers/$container/stop")
    assert_api_success "$response" "Stop container endpoint returns success"

    # Verify operation_id is returned
    local op_id=$(echo "$response" | jq -r '.data.operation_id')
    if [ -n "$op_id" ] && [ "$op_id" != "null" ]; then
        print_success "Stop operation returned operation_id: ${op_id:0:8}..."
        STOP_OPERATION_ID="$op_id"
    else
        print_error "Stop operation did not return operation_id"
    fi

    sleep 2

    # Verify container is stopped
    local new_state=$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null || echo "true")
    if [ "$new_state" = "false" ]; then
        print_success "Container stopped successfully"
    else
        print_error "Container is still running after stop"
    fi

    # Verify stop operation was recorded in history
    local ops_response=$(curl_api GET "/operations?limit=5")
    local stop_op=$(echo "$ops_response" | jq -r '.data.operations[] | select(.operation_type == "stop" and .container_name == "'"$container"'") | .operation_id')
    if [ -n "$stop_op" ] && [ "$stop_op" != "null" ]; then
        print_success "Stop operation recorded in history"
    else
        print_error "Stop operation not found in history"
    fi

    # Start container again for subsequent tests
    docker start "$container" 2>/dev/null || true
    sleep 3
}

# Test 16: POST /api/containers/{name}/stop (already stopped - should fail)
test_stop_already_stopped() {
    print_info "Test: POST /api/containers/{name}/stop (already stopped)"

    # Create a temporary stopped container for this test
    local container="test-stop-temp"
    docker run -d --name "$container" alpine:latest sleep 3600 2>/dev/null || true
    sleep 1
    docker stop "$container" 2>/dev/null || true
    sleep 1

    # Try to stop already stopped container
    local response=$(curl_api POST "/containers/$container/stop")
    local success=$(echo "$response" | jq -r '.success')

    if [ "$success" = "false" ]; then
        print_success "Stop on already stopped container correctly returns error"
    else
        print_error "Stop on already stopped container should have failed"
    fi

    # Cleanup
    docker rm -f "$container" 2>/dev/null || true
}

# Test 17: DELETE /api/containers/{name} (remove)
test_remove_container() {
    print_info "Test: DELETE /api/containers/{name}"

    # Create a temporary container for removal test
    local container="test-remove-temp"
    docker run -d --name "$container" alpine:latest sleep 3600 2>/dev/null || true
    sleep 2

    # Stop it first (remove without force requires stopped container)
    docker stop "$container" 2>/dev/null || true
    sleep 1

    local response=$(curl_api DELETE "/containers/$container")
    assert_api_success "$response" "Remove container endpoint returns success"

    # Verify operation_id is returned
    local op_id=$(echo "$response" | jq -r '.data.operation_id')
    if [ -n "$op_id" ] && [ "$op_id" != "null" ]; then
        print_success "Remove operation returned operation_id: ${op_id:0:8}..."
        REMOVE_OPERATION_ID="$op_id"
    else
        print_error "Remove operation did not return operation_id"
    fi

    sleep 1

    # Verify container is removed
    if ! docker inspect "$container" &>/dev/null; then
        print_success "Container removed successfully"
    else
        print_error "Container still exists after remove"
        docker rm -f "$container" 2>/dev/null || true
    fi

    # Verify remove operation was recorded in history
    local ops_response=$(curl_api GET "/operations?limit=5")
    local remove_op=$(echo "$ops_response" | jq -r '.data.operations[] | select(.operation_type == "remove" and .container_name == "'"$container"'") | .operation_id')
    if [ -n "$remove_op" ] && [ "$remove_op" != "null" ]; then
        print_success "Remove operation recorded in history"
    else
        print_error "Remove operation not found in history"
    fi
}

# Test 18: DELETE /api/containers/{name}?force=true (force remove running)
test_remove_container_force() {
    print_info "Test: DELETE /api/containers/{name}?force=true"

    # Create a running container
    local container="test-remove-force-temp"
    docker run -d --name "$container" alpine:latest sleep 3600 2>/dev/null || true
    sleep 2

    # Force remove running container
    local response=$(curl_api DELETE "/containers/$container?force=true")
    assert_api_success "$response" "Force remove container endpoint returns success"

    sleep 1

    # Verify container is removed
    if ! docker inspect "$container" &>/dev/null; then
        print_success "Running container force removed successfully"
    else
        print_error "Container still exists after force remove"
        docker rm -f "$container" 2>/dev/null || true
    fi
}

# Test 19: POST /api/update/batch
test_batch_update() {
    print_info "Test: POST /api/update/batch"

    # Update both nginx and redis in batch
    local body='{"containers":[{"name":"test-nginx-basic","target_version":"1.29.3"},{"name":"test-redis-basic","target_version":"8.4"}]}'
    local response=$(curl_api POST "/update/batch" "$body")
    assert_api_success "$response" "Batch update initiated"

    # Get all operation IDs from the operations array
    local op_ids=$(echo "$response" | jq -r '.data.operations[].operation_id // empty')
    print_info "Batch operation IDs: $op_ids"

    # Wait for each operation to complete
    for op_id in $op_ids; do
        if [ -n "$op_id" ] && [ "$op_id" != "null" ]; then
            wait_for_operation "$op_id" 180
        fi
    done

    # Verify versions
    assert_version "test-nginx-basic" "1.29.3" "Nginx updated to 1.29.3"
    assert_version "test-redis-basic" "8.4" "Redis updated to 8.4"
}

# Main test execution
main() {
    check_docksmith || exit 1

    # Setup environment
    setup

    print_info "Using test container: $TEST_CONTAINER"
    print_info "Using environment: $TEST_STACK"
    echo ""

    # Trigger initial discovery so containers are in status
    print_info "Triggering initial discovery..."
    curl_api POST "/trigger-check" > /dev/null
    sleep 5

    # Run all tests
    test_health
    test_docker_config
    test_status
    test_check
    test_trigger_check
    test_update_single
    test_operations
    test_operation_by_id
    test_rollback
    test_history
    test_get_labels
    test_set_labels
    test_remove_labels
    test_restart_container
    test_stop_container
    test_stop_already_stopped
    test_remove_container
    test_remove_container_force
    test_batch_update

    # Print summary
    print_test_summary
}

main "$@"

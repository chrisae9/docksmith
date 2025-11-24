#!/bin/bash
# Test Docksmith label functionality
# Tests all supported Docksmith labels
# This script is self-contained: it sets up, tests, and cleans up its own environment

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_DIR="$SCRIPT_DIR/../environments"
source "$SCRIPT_DIR/helpers.sh"

# Test configuration
TEST_STACK="labels"
ENV_PATH="$ENV_DIR/$TEST_STACK"

# Setup function
setup() {
    print_header "Setting up label test environment"

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
    print_header "Cleaning up label test environment"

    if [ -d "$ENV_PATH" ]; then
        print_info "Stopping $TEST_STACK containers..."
        (cd "$ENV_PATH" && docker compose down 2>&1 | grep -E "(Stopping|Removing|Network)" || true)
    fi
}

# Trap cleanup on exit
trap cleanup EXIT

print_header "Testing Docksmith Label Functionality"

# Test 1: docksmith.ignore label
test_ignore_label() {
    print_info "Test: docksmith.ignore label"

    local container="test-labels-ignored"

    # Trigger initial scan to ensure Docksmith has discovered all containers
    print_info "Triggering initial discovery scan..."
    curl_api POST "/trigger-check" > /dev/null
    sleep 5

    # Verify container has IGNORED status (has ignore label from compose)
    assert_status "$container" "IGNORED" "Container with docksmith.ignore has IGNORED status"

    # Remove ignore label
    print_info "Removing ignore label..."
    local body='{"container":"'"$container"'","label_names":["docksmith.ignore"],"force":true}'
    local response=$(curl_api POST "/labels/remove" "$body")
    assert_api_success "$response" "Ignore label removed"

    sleep 8

    # Trigger check to refresh discovery
    curl_api POST "/trigger-check" > /dev/null
    sleep 5

    # Verify container NOW appears in discovery
    assert_container_exists "$container" "Container appears after ignore label removed"

    # Re-add ignore label
    print_info "Re-adding ignore label..."
    local body='{"container":"'"$container"'","ignore":true,"force":true}'
    local response=$(curl_api POST "/labels/set" "$body")
    assert_api_success "$response" "Ignore label re-added"

    sleep 8

    # Trigger check to refresh discovery
    curl_api POST "/trigger-check" > /dev/null
    sleep 5

    # Verify container has IGNORED status again
    assert_status "$container" "IGNORED" "Container has IGNORED status after ignore label re-added"
}

# Test 2: docksmith.allow-latest label
test_allow_latest_label() {
    print_info "Test: docksmith.allow-latest label"

    local container="test-labels-latest"

    # Check that container has allow-latest label
    local labels_response=$(curl_api GET "/labels/$container")
    local allow_latest=$(echo "$labels_response" | jq -r '.data.labels."docksmith.allow-latest"')

    if [ "$allow_latest" = "true" ]; then
        print_success "Container has docksmith.allow-latest=true"
    else
        print_error "Container missing docksmith.allow-latest label"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Container should appear in discovery without warnings
    assert_container_exists "$container" "Container with :latest tag and allow-latest label in discovery"
}

# Test 3: docksmith.pre-update-check label (passing script)
test_pre_check_pass() {
    print_info "Test: docksmith.pre-update-check (passing script)"

    local container="test-labels-pre-pass"

    # Verify container has pre-update-check label
    local labels_response=$(curl_api GET "/labels/$container")
    local pre_check=$(echo "$labels_response" | jq -r '.data.labels."docksmith.pre-update-check"')

    if [ -n "$pre_check" ] && [ "$pre_check" != "null" ]; then
        print_success "Container has docksmith.pre-update-check label: $pre_check"
    else
        print_error "Container missing pre-update-check label"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi

    # Attempt update (should succeed because script returns 0)
    print_info "Attempting update (should pass pre-check)..."
    local body='{"container_name":"'"$container"'","target_version":"8.4"}'
    local response=$(curl_api POST "/update" "$body")

    # Should succeed
    assert_api_success "$response" "Update with passing pre-check succeeds"

    sleep 10

    # Verify updated
    assert_version "$container" "8.4" "Container updated after passing pre-check"
}

# Test 4: docksmith.pre-update-check label (failing script)
test_pre_check_fail() {
    print_info "Test: docksmith.pre-update-check (failing script)"

    local container="test-labels-pre-fail"

    # Attempt update (should fail because script returns 1)
    print_info "Attempting update (should fail pre-check)..."
    local body='{"container_name":"'"$container"'","target_version":"8.4"}'
    local response=$(curl_api POST "/update" "$body")

    # The API should return success (operation started)
    assert_api_success "$response" "Update operation started"

    # Extract operation ID and wait for it to fail
    local op_id=$(echo "$response" | jq -r '.data.operation_id')
    print_info "Waiting for operation $op_id to fail due to pre-check..."
    sleep 5

    # Check operation status
    local op_response=$(curl_api GET "/operations/$op_id")
    local op_status=$(echo "$op_response" | jq -r '.data.status')
    local op_error=$(echo "$op_response" | jq -r '.data.error_message // ""')

    TESTS_RUN=$((TESTS_RUN + 1))

    if [ "$op_status" = "failed" ] && [[ "$op_error" == *"Pre-update check failed"* ]]; then
        print_success "Update blocked by failing pre-check script"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Update should have been blocked by pre-check (status: $op_status, error: $op_error)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Verify still on old version (latest tag from compose file)
    assert_version "$container" "latest" "Container remains on old version after failed pre-check"
}

# Test 5: docksmith.restart-after label
test_restart_after() {
    print_info "Test: docksmith.restart-after label"

    local primary="test-labels-restart-deps"
    local dep1="test-labels-dependent-1"
    local dep2="test-labels-dependent-2"

    # Get initial restart times
    local primary_time_before=$(get_restart_time "$primary")
    local dep1_time_before=$(get_restart_time "$dep1")
    local dep2_time_before=$(get_restart_time "$dep2")

    # Restart primary container
    print_info "Restarting primary container..."
    local response=$(curl_api POST "/restart/container/$primary")
    assert_api_success "$response" "Primary container restart initiated"

    sleep 10

    # Get new restart times
    local primary_time_after=$(get_restart_time "$primary")
    local dep1_time_after=$(get_restart_time "$dep1")
    local dep2_time_after=$(get_restart_time "$dep2")

    # Verify all were restarted
    TESTS_RUN=$((TESTS_RUN + 3))

    if [ "$primary_time_after" != "$primary_time_before" ]; then
        print_success "Primary container was restarted"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Primary container was not restarted"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    if [ "$dep1_time_after" != "$dep1_time_before" ]; then
        print_success "Dependent-1 was restarted"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Dependent-1 was not restarted"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    if [ "$dep2_time_after" != "$dep2_time_before" ]; then
        print_success "Dependent-2 was restarted"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Dependent-2 was not restarted"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# Test 6: Label atomicity (set label via API)
test_label_atomicity() {
    print_info "Test: Label atomicity (compose file + container restart)"

    local container="test-labels-restart-deps"  # From labels environment

    # Set a label via API
    print_info "Setting label via API..."
    local body='{"container":"'"$container"'","allow_latest":true,"no_restart":true}'
    local response=$(curl_api POST "/labels/set" "$body")
    assert_api_success "$response" "Label set via API"

    sleep 5

    # Verify label exists
    local labels_response=$(curl_api GET "/labels/$container")
    local allow_latest=$(echo "$labels_response" | jq -r '.data.labels."docksmith.allow-latest"')

    TESTS_RUN=$((TESTS_RUN + 1))

    if [ "$allow_latest" = "true" ]; then
        print_success "Label persisted in compose file and container"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Label not persisted correctly"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Clean up - remove label
    local body='{"container":"'"$container"'","label_names":["docksmith.allow-latest"],"no_restart":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null
    sleep 3
}

# Main test execution
main() {
    check_docksmith || exit 1

    # Setup environment
    setup

    print_info "Using environment: $TEST_STACK"
    echo ""

    # Run all label tests
    test_ignore_label
    test_allow_latest_label
    test_pre_check_pass
    test_pre_check_fail
    test_restart_after
    test_label_atomicity

    # Print summary
    print_test_summary
}

main "$@"

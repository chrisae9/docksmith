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

# Test 7: docksmith.version-pin-minor label
test_version_pin_minor() {
    print_info "Test: docksmith.version-pin-minor label"

    # Setup: Use nginx container on 1.25.3, with available versions: 1.25.4, 1.26.0, 1.27.0
    local container="test-labels-nginx"

    # Set version-pin-minor label
    print_info "Setting version-pin-minor label..."
    local body='{"container":"'"$container"'","version_pin_minor":true}'
    local response=$(curl_api POST "/labels/set" "$body")
    assert_api_success "$response" "Version-pin-minor label set"

    sleep 5

    # Trigger check
    curl_api POST "/trigger-check" > /dev/null
    sleep 5

    # Check container status
    local status_response=$(curl_api GET "/status")
    local latest_version=$(echo "$status_response" | jq -r '.data.containers[] | select(.container_name=="'"$container"'") | .latest_version')

    TESTS_RUN=$((TESTS_RUN + 1))

    # Should suggest 1.25.4 (same minor), NOT 1.26.0 or 1.27.0
    if [[ "$latest_version" == "1.25."* ]]; then
        print_success "Version pinning to minor version works (got: $latest_version)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Version pinning failed - got $latest_version, expected 1.25.x"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Clean up
    local body='{"container":"'"$container"'","label_names":["docksmith.version-pin-minor"],"no_restart":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null
    sleep 2
}

# Test 8: docksmith.tag-regex label
test_tag_regex() {
    print_info "Test: docksmith.tag-regex label"

    local container="test-labels-alpine"

    # Set tag-regex to only allow alpine tags (Node.js has versioned -alpine tags)
    print_info "Setting tag-regex for Alpine builds only..."
    local body='{"container":"'"$container"'","tag_regex":"^[0-9]+-alpine$"}'
    local response=$(curl_api POST "/labels/set" "$body")
    assert_api_success "$response" "Tag-regex label set"

    sleep 5

    # Trigger check
    curl_api POST "/trigger-check" > /dev/null
    sleep 5

    # Check container status
    local status_response=$(curl_api GET "/status")
    local latest_version=$(echo "$status_response" | jq -r '.data.containers[] | select(.container_name=="'"$container"'") | .latest_version')

    TESTS_RUN=$((TESTS_RUN + 1))

    # Should only suggest alpine tags
    if [[ "$latest_version" == *"-alpine" ]]; then
        print_success "Tag regex filtering works (got alpine tag: $latest_version)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Tag regex failed - got $latest_version, expected *-alpine"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Clean up
    local body='{"container":"'"$container"'","label_names":["docksmith.tag-regex"],"no_restart":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null
    sleep 2
}

# Test 9: docksmith.version-min label
test_version_min() {
    print_info "Test: docksmith.version-min label"

    local container="test-labels-postgres"

    # Set minimum version to 14.0
    print_info "Setting version-min to 14.0..."
    local body='{"container":"'"$container"'","version_min":"14.0"}'
    local response=$(curl_api POST "/labels/set" "$body")
    assert_api_success "$response" "Version-min label set"

    sleep 5

    # Trigger check
    curl_api POST "/trigger-check" > /dev/null
    sleep 5

    # Check container status
    local status_response=$(curl_api GET "/status")
    local latest_version=$(echo "$status_response" | jq -r '.data.containers[] | select(.container_name=="'"$container"'") | .latest_version')

    TESTS_RUN=$((TESTS_RUN + 1))

    # Should NOT suggest versions below 14.0
    local major_version=$(echo "$latest_version" | cut -d. -f1)
    if [ "$major_version" -ge 14 ]; then
        print_success "Version-min filter works (got: $latest_version >= 14.0)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Version-min failed - got $latest_version, expected >= 14.0"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Clean up
    local body='{"container":"'"$container"'","label_names":["docksmith.version-min"],"no_restart":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null
    sleep 2
}

# Test 10: docksmith.version-max label
test_version_max() {
    print_info "Test: docksmith.version-max label"

    local container="test-labels-redis"

    # Set maximum version to 7.99
    print_info "Setting version-max to 7.99..."
    local body='{"container":"'"$container"'","version_max":"7.99"}'
    local response=$(curl_api POST "/labels/set" "$body")
    assert_api_success "$response" "Version-max label set"

    sleep 5

    # Trigger check
    curl_api POST "/trigger-check" > /dev/null
    sleep 5

    # Check container status
    local status_response=$(curl_api GET "/status")
    local latest_version=$(echo "$status_response" | jq -r '.data.containers[] | select(.container_name=="'"$container"'") | .latest_version')

    TESTS_RUN=$((TESTS_RUN + 1))

    # Should NOT suggest version 8.x or higher
    local major_version=$(echo "$latest_version" | cut -d. -f1)
    if [ "$major_version" -le 7 ]; then
        print_success "Version-max filter works (got: $latest_version <= 7.99)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Version-max failed - got $latest_version, expected <= 7.99"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Clean up
    local body='{"container":"'"$container"'","label_names":["docksmith.version-max"],"no_restart":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null
    sleep 2
}

# Test 11: Invalid regex pattern validation
test_invalid_regex() {
    print_info "Test: Invalid regex pattern validation"

    local container="test-labels-nginx"

    # Try to set invalid regex
    print_info "Attempting to set invalid regex pattern..."
    local body='{"container":"'"$container"'","tag_regex":"(invalid[regex","no_restart":true}'
    local response=$(curl_api POST "/labels/set" "$body")

    TESTS_RUN=$((TESTS_RUN + 1))

    # Should fail with error about invalid regex
    local success=$(echo "$response" | jq -r '.success')
    local error=$(echo "$response" | jq -r '.error // ""')

    if [ "$success" = "false" ] && [[ "$error" == *"invalid regex"* ]]; then
        print_success "Invalid regex rejected by validation"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Invalid regex should have been rejected"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# Test 12: Regex pattern too long validation
test_regex_too_long() {
    print_info "Test: Regex pattern length validation"

    local container="test-labels-nginx"

    # Create a pattern that's over 500 characters
    local long_pattern=$(printf 'a%.0s' {1..501})

    # Try to set overly long regex
    print_info "Attempting to set overly long regex pattern (>500 chars)..."
    local body='{"container":"'"$container"'","tag_regex":"'"$long_pattern"'","no_restart":true}'
    local response=$(curl_api POST "/labels/set" "$body")

    TESTS_RUN=$((TESTS_RUN + 1))

    # Should fail with error about pattern length
    local success=$(echo "$response" | jq -r '.success')
    local error=$(echo "$response" | jq -r '.error // ""')

    if [ "$success" = "false" ] && [[ "$error" == *"too long"* ]]; then
        print_success "Overly long regex rejected by validation"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Overly long regex should have been rejected"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# Test 13: Multiple constraints combined
test_combined_constraints() {
    print_info "Test: Multiple version constraints combined"

    local container="test-labels-node"

    # Set both pin-minor and version-max
    print_info "Setting multiple constraints (pin-minor + version-max)..."
    local body='{"container":"'"$container"'","version_pin_minor":true,"version_max":"20.99","no_restart":true}'
    local response=$(curl_api POST "/labels/set" "$body")
    assert_api_success "$response" "Multiple constraints set"

    sleep 3

    # Verify both labels persisted
    local labels_response=$(curl_api GET "/labels/$container")
    local pin_minor=$(echo "$labels_response" | jq -r '.data.labels."docksmith.version-pin-minor"')
    local version_max=$(echo "$labels_response" | jq -r '.data.labels."docksmith.version-max"')

    TESTS_RUN=$((TESTS_RUN + 2))

    if [ "$pin_minor" = "true" ]; then
        print_success "Pin-minor label persisted"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Pin-minor label not persisted"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    if [ "$version_max" = "20.99" ]; then
        print_success "Version-max label persisted"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        print_error "Version-max label not persisted"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Clean up
    local body='{"container":"'"$container"'","label_names":["docksmith.version-pin-minor","docksmith.version-max"],"no_restart":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null
    sleep 2
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

    # Run new version constraint tests
    test_version_pin_minor
    test_tag_regex
    test_version_min
    test_version_max
    test_invalid_regex
    test_regex_too_long
    test_combined_constraints

    # Print summary
    print_test_summary
}

main "$@"

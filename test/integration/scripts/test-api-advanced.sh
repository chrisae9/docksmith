#!/bin/bash
# Test additional Docksmith API endpoints not covered by test-api.sh
# Covers: policies, scripts, registry tags, stack restart, SSE events

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_DIR="$SCRIPT_DIR/../environments"
source "$SCRIPT_DIR/helpers.sh"

# Test configuration
TEST_STACK="multi-stack"
ENV_PATH="$ENV_DIR/$TEST_STACK"

# Setup function
setup() {
    print_header "Setting up Advanced API test environment"

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
    print_header "Cleaning up Advanced API test environment"

    if [ -d "$ENV_PATH" ]; then
        print_info "Stopping $TEST_STACK containers..."
        (cd "$ENV_PATH" && docker compose down 2>&1 | grep -E "(Stopping|Removing|Network)" || true)
    fi
}

# Trap cleanup on exit
trap cleanup EXIT

print_header "Testing Advanced Docksmith API Endpoints"

# Test 1: GET /api/policies
test_policies() {
    print_info "Test: GET /api/policies"
    local response=$(curl_api GET "/policies")
    assert_api_success "$response" "Policies endpoint returns success"

    # Verify global_policy field exists
    local has_global=$(echo "$response" | jq -e '.data.global_policy != null' 2>/dev/null || echo "false")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$has_global" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Policies response contains global_policy field"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Policies response missing global_policy field"
    fi
}

# Test 2: GET /api/scripts
test_scripts_list() {
    print_info "Test: GET /api/scripts"
    local response=$(curl_api GET "/scripts")
    assert_api_success "$response" "Scripts list endpoint returns success"

    # Verify scripts array exists and has entries (we have scripts in /scripts folder)
    local count=$(echo "$response" | jq -r '.data.count // 0')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$count" -gt 0 ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Found $count available scripts"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "No scripts found (expected scripts in /scripts folder)"
    fi
}

# Test 3: GET /api/scripts/assigned
test_scripts_assigned() {
    print_info "Test: GET /api/scripts/assigned"
    local response=$(curl_api GET "/scripts/assigned")
    assert_api_success "$response" "Scripts assigned endpoint returns success"

    # Verify assignments array exists
    local has_assignments=$(echo "$response" | jq -e '.data.assignments != null' 2>/dev/null || echo "false")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$has_assignments" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Scripts assigned response contains assignments array"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Scripts assigned response missing assignments array"
    fi
}

# Test 4: POST /api/scripts/assign
test_scripts_assign() {
    print_info "Test: POST /api/scripts/assign"

    # Assign check-always-pass.sh to rooday2-nginx
    # Note: script_path should be relative to /scripts directory
    local body='{"container_name":"rooday2-nginx","script_path":"check-always-pass.sh"}'
    local response=$(curl_api POST "/scripts/assign" "$body")
    assert_api_success "$response" "Scripts assign endpoint returns success"

    # Verify assignment message
    local message=$(echo "$response" | jq -r '.data.message // ""')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [[ "$message" == *"assigned successfully"* ]]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Script assignment confirmed"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Script assignment message not as expected: $message"
    fi

    # Verify it appears in assigned list
    sleep 1
    local assigned_response=$(curl_api GET "/scripts/assigned")
    local found=$(echo "$assigned_response" | jq -r '.data.assignments[] | select(.container_name == "rooday2-nginx") | .container_name' 2>/dev/null || echo "")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$found" = "rooday2-nginx" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Assignment appears in assigned list"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Assignment not found in assigned list"
    fi
}

# Test 5: DELETE /api/scripts/assign/{container}
test_scripts_unassign() {
    print_info "Test: DELETE /api/scripts/assign/{container}"

    # Unassign from rooday2-nginx
    local response=$(curl_api DELETE "/scripts/assign/rooday2-nginx")
    assert_api_success "$response" "Scripts unassign endpoint returns success"

    # Verify unassignment message
    local message=$(echo "$response" | jq -r '.data.message // ""')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [[ "$message" == *"unassigned successfully"* ]]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Script unassignment confirmed"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Script unassignment message not as expected: $message"
    fi

    # Verify it's removed from assigned list
    sleep 1
    local assigned_response=$(curl_api GET "/scripts/assigned")
    local found=$(echo "$assigned_response" | jq -r '.data.assignments[] | select(.container_name == "rooday2-nginx") | .container_name' 2>/dev/null || echo "")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ -z "$found" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Assignment removed from assigned list"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Assignment still found in assigned list after unassign"
    fi
}

# Test 6: POST /api/scripts/assign - validation errors
test_scripts_assign_validation() {
    print_info "Test: POST /api/scripts/assign - validation"

    # Test missing container_name
    local body='{"script_path":"/scripts/check-always-pass.sh"}'
    local response=$(curl_api POST "/scripts/assign" "$body")
    local success=$(echo "$response" | jq -r '.success')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$success" = "false" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Assign rejects missing container_name"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Assign should reject missing container_name"
    fi

    # Test missing script_path
    body='{"container_name":"rooday2-nginx"}'
    response=$(curl_api POST "/scripts/assign" "$body")
    success=$(echo "$response" | jq -r '.success')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$success" = "false" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Assign rejects missing script_path"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Assign should reject missing script_path"
    fi
}

# Test 7: GET /api/registry/tags/{imageRef}
test_registry_tags() {
    print_info "Test: GET /api/registry/tags/{imageRef}"

    # Test with nginx image (should have tags in cache after discovery)
    local response=$(curl_api GET "/registry/tags/nginx")
    assert_api_success "$response" "Registry tags endpoint returns success"

    # Verify tags array exists
    local has_tags=$(echo "$response" | jq -e '.data.tags != null' 2>/dev/null || echo "false")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$has_tags" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Registry tags response contains tags array"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Registry tags response missing tags array"
    fi

    # Verify image_ref in response
    local image_ref=$(echo "$response" | jq -r '.data.image_ref // ""')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$image_ref" = "nginx" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Registry tags returns correct image_ref"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Registry tags image_ref mismatch: $image_ref"
    fi
}

# Test 8: POST /api/restart/stack/{name}
test_restart_stack() {
    print_info "Test: POST /api/restart/stack/{name}"

    # Get restart times before
    local nginx_old_time=$(get_restart_time "rooday2-nginx")
    local traefik_old_time=$(get_restart_time "rooday2-traefik")

    local response=$(curl_api POST "/restart/stack/$TEST_STACK")
    assert_api_success "$response" "Restart stack endpoint returns success"

    # Wait for restarts to complete
    sleep 10

    # Verify at least one container was restarted
    local nginx_new_time=$(get_restart_time "rooday2-nginx")
    local traefik_new_time=$(get_restart_time "rooday2-traefik")

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$nginx_new_time" != "$nginx_old_time" ] || [ "$traefik_new_time" != "$traefik_old_time" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Stack containers were restarted"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Stack container restart times unchanged"
    fi

    # Verify response structure
    local container_count=$(echo "$response" | jq -r '.data.container_names | length // 0')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$container_count" -gt 0 ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Restart response includes container_names ($container_count containers)"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Restart response missing container_names"
    fi
}

# Test 9: POST /api/restart/stack/{name} - not found
test_restart_stack_not_found() {
    print_info "Test: POST /api/restart/stack/{name} - not found"

    local response=$(curl_api POST "/restart/stack/nonexistent-stack-xyz")
    local success=$(echo "$response" | jq -r '.success')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$success" = "false" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Restart stack returns error for nonexistent stack"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Restart stack should fail for nonexistent stack"
    fi
}

# Test 10: POST /api/restart/start/{name} (SSE-based restart)
test_restart_start_sse() {
    print_info "Test: POST /api/restart/start/{name}"

    local response=$(curl_api POST "/restart/start/rooday2-redis")
    assert_api_success "$response" "SSE restart endpoint returns success"

    # Verify operation_id is returned
    local op_id=$(echo "$response" | jq -r '.data.operation_id // ""')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ -n "$op_id" ] && [ "$op_id" != "null" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "SSE restart returns operation_id: $op_id"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "SSE restart missing operation_id"
    fi

    # Wait for restart to complete
    sleep 5
}

# Test 11: POST /api/restart (body-based)
test_restart_body() {
    print_info "Test: POST /api/restart (body-based)"

    local old_time=$(get_restart_time "rooday2-postgres")

    local body='{"container_name":"rooday2-postgres"}'
    local response=$(curl_api POST "/restart" "$body")
    assert_api_success "$response" "Body-based restart endpoint returns success"

    sleep 5

    local new_time=$(get_restart_time "rooday2-postgres")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$new_time" != "$old_time" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Container restarted via body-based endpoint"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Container restart time unchanged"
    fi
}

# Test 12: GET /api/events (SSE connection test)
test_events_sse() {
    print_info "Test: GET /api/events (SSE)"

    # Connect to SSE endpoint with timeout, check for initial connection event
    local response=$(timeout 3 curl -s -N "${API_BASE}/events" 2>/dev/null | head -5 || true)

    TESTS_RUN=$((TESTS_RUN + 1))
    if echo "$response" | grep -q "event: connected"; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "SSE endpoint sends connection event"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "SSE endpoint did not send connection event"
        print_info "Response: $response"
    fi
}

# Test 13: Negative test - invalid operation ID for rollback
test_rollback_invalid_id() {
    print_info "Test: POST /api/rollback - invalid operation ID"

    local body='{"operation_id":"nonexistent-operation-id-xyz"}'
    local response=$(curl_api POST "/rollback" "$body")
    local success=$(echo "$response" | jq -r '.success')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$success" = "false" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Rollback fails for invalid operation ID"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Rollback should fail for invalid operation ID"
    fi
}

# Test 14: Negative test - restart nonexistent container
test_restart_container_not_found() {
    print_info "Test: POST /api/restart/container/{name} - not found"

    local response=$(curl_api POST "/restart/container/nonexistent-container-xyz")
    local success=$(echo "$response" | jq -r '.success')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$success" = "false" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Restart fails for nonexistent container"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Restart should fail for nonexistent container"
    fi
}

# Main test execution
main() {
    check_docksmith || exit 1

    # Setup environment
    setup

    print_info "Using test stack: $TEST_STACK"
    echo ""

    # Trigger initial discovery so containers are in status
    print_info "Triggering initial discovery..."
    curl_api GET "/check" > /dev/null
    sleep 5

    # Run all tests
    test_policies
    test_scripts_list
    test_scripts_assigned
    test_scripts_assign
    test_scripts_unassign
    test_scripts_assign_validation
    test_registry_tags
    test_restart_stack
    test_restart_stack_not_found
    test_restart_start_sse
    test_restart_body
    test_events_sse
    test_rollback_invalid_id
    test_restart_container_not_found

    # Print summary
    print_test_summary
}

main "$@"

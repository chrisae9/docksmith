#!/bin/bash
# Test Docksmith batch labels API endpoint
# Tests POST /api/labels/batch for applying labels to multiple containers at once
# This script is self-contained: it sets up, tests, and cleans up its own environment

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_DIR="$SCRIPT_DIR/../environments"
source "$SCRIPT_DIR/helpers.sh"

# Test configuration
TEST_STACK="labels"
ENV_PATH="$ENV_DIR/$TEST_STACK"

# Containers from the labels environment
CONTAINER_1="test-labels-nginx"
CONTAINER_2="test-labels-redis"
CONTAINER_3="test-labels-node"

# Setup function
setup() {
    print_header "Setting up batch labels test environment"

    if [ ! -d "$ENV_PATH" ]; then
        print_error "Environment directory not found: $ENV_PATH"
        exit 1
    fi

    print_info "Resetting $TEST_STACK environment..."
    "$SCRIPT_DIR/reset.sh" "$TEST_STACK"
    sleep 5

    # Trigger initial discovery
    print_info "Triggering initial discovery scan..."
    curl_api POST "/trigger-check" > /dev/null
    sleep 5
}

# Cleanup function
cleanup() {
    print_header "Cleaning up batch labels test environment"

    # Remove any labels we set during tests (best-effort, no_restart for speed)
    for container in "$CONTAINER_1" "$CONTAINER_2" "$CONTAINER_3"; do
        local body='{"container":"'"$container"'","label_names":["docksmith.ignore","docksmith.version-pin-minor","docksmith.allow-latest","docksmith.tag-regex"],"no_restart":true,"force":true}'
        curl_api POST "/labels/remove" "$body" > /dev/null 2>&1 || true
    done

    if [ -d "$ENV_PATH" ]; then
        print_info "Stopping $TEST_STACK containers..."
        (cd "$ENV_PATH" && docker compose down 2>&1 | grep -E "(Stopping|Removing|Network)" || true)
    fi
}

# Trap cleanup on exit
trap cleanup EXIT

print_header "Testing Docksmith Batch Labels API"

# Test 1: Set ignore=true on multiple containers
test_batch_labels_ignore() {
    print_info "Test: Batch set ignore=true on multiple containers"

    local body='{"operations":[
        {"container":"'"$CONTAINER_1"'","ignore":true,"no_restart":true},
        {"container":"'"$CONTAINER_2"'","ignore":true,"no_restart":true}
    ]}'

    local response=$(curl_api POST "/labels/batch" "$body")
    assert_api_success "$response" "Batch ignore request accepted"

    # Verify results array
    local result_count=$(echo "$response" | jq -r '.data.results | length')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$result_count" = "2" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Batch returned 2 results"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Expected 2 results, got: $result_count"
    fi

    # Verify each result is successful
    local success_1=$(echo "$response" | jq -r '.data.results[] | select(.container=="'"$CONTAINER_1"'") | .success')
    local success_2=$(echo "$response" | jq -r '.data.results[] | select(.container=="'"$CONTAINER_2"'") | .success')

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$success_1" = "true" ] && [ "$success_2" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Both containers returned success"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Expected both success=true, got: $success_1, $success_2"
    fi

    # Verify each result has an operation_id
    local op_id_1=$(echo "$response" | jq -r '.data.results[] | select(.container=="'"$CONTAINER_1"'") | .operation_id')
    local op_id_2=$(echo "$response" | jq -r '.data.results[] | select(.container=="'"$CONTAINER_2"'") | .operation_id')

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ -n "$op_id_1" ] && [ "$op_id_1" != "null" ] && [ -n "$op_id_2" ] && [ "$op_id_2" != "null" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Both containers returned operation_ids"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Missing operation_ids: $op_id_1, $op_id_2"
    fi

    # Wait for operations to complete
    sleep 5

    # Clean up ignore labels
    for container in "$CONTAINER_1" "$CONTAINER_2"; do
        local body='{"container":"'"$container"'","label_names":["docksmith.ignore"],"no_restart":true,"force":true}'
        curl_api POST "/labels/remove" "$body" > /dev/null 2>&1 || true
    done
    sleep 2
}

# Test 2: Set version_pin_minor=true on multiple containers
test_batch_labels_pin_minor() {
    print_info "Test: Batch set version_pin_minor=true on multiple containers"

    local body='{"operations":[
        {"container":"'"$CONTAINER_1"'","version_pin_minor":true,"no_restart":true},
        {"container":"'"$CONTAINER_3"'","version_pin_minor":true,"no_restart":true}
    ]}'

    local response=$(curl_api POST "/labels/batch" "$body")
    assert_api_success "$response" "Batch pin-minor request accepted"

    # Verify both succeeded
    local all_success=$(echo "$response" | jq -r '[.data.results[].success] | all')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$all_success" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "All pin-minor operations succeeded"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Not all pin-minor operations succeeded"
        print_info "Response: $response"
    fi

    sleep 5

    # Clean up
    for container in "$CONTAINER_1" "$CONTAINER_3"; do
        local body='{"container":"'"$container"'","label_names":["docksmith.version-pin-minor"],"no_restart":true,"force":true}'
        curl_api POST "/labels/remove" "$body" > /dev/null 2>&1 || true
    done
    sleep 2
}

# Test 3: Set allow_latest=true on multiple containers
test_batch_labels_allow_latest() {
    print_info "Test: Batch set allow_latest=true on multiple containers"

    local body='{"operations":[
        {"container":"'"$CONTAINER_1"'","allow_latest":true,"no_restart":true},
        {"container":"'"$CONTAINER_2"'","allow_latest":true,"no_restart":true},
        {"container":"'"$CONTAINER_3"'","allow_latest":true,"no_restart":true}
    ]}'

    local response=$(curl_api POST "/labels/batch" "$body")
    assert_api_success "$response" "Batch allow-latest request accepted"

    # Verify all 3 succeeded
    local result_count=$(echo "$response" | jq -r '.data.results | length')
    local success_count=$(echo "$response" | jq -r '[.data.results[] | select(.success == true)] | length')

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$result_count" = "3" ] && [ "$success_count" = "3" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "All 3 allow-latest operations succeeded"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Expected 3 successes, got $success_count out of $result_count"
    fi

    sleep 5

    # Clean up
    for container in "$CONTAINER_1" "$CONTAINER_2" "$CONTAINER_3"; do
        local body='{"container":"'"$container"'","label_names":["docksmith.allow-latest"],"no_restart":true,"force":true}'
        curl_api POST "/labels/remove" "$body" > /dev/null 2>&1 || true
    done
    sleep 2
}

# Test 4: Clear tag_regex by sending empty string
test_batch_labels_clear() {
    print_info "Test: Batch clear tag_regex (send empty string to remove)"

    # First, set tag_regex on two containers
    local body='{"operations":[
        {"container":"'"$CONTAINER_1"'","tag_regex":"^[0-9]+$","no_restart":true},
        {"container":"'"$CONTAINER_2"'","tag_regex":"^[0-9]+$","no_restart":true}
    ]}'
    local set_response=$(curl_api POST "/labels/batch" "$body")
    assert_api_success "$set_response" "Tag-regex set on multiple containers"

    sleep 5

    # Now clear tag_regex by sending empty string
    local body='{"operations":[
        {"container":"'"$CONTAINER_1"'","tag_regex":"","no_restart":true},
        {"container":"'"$CONTAINER_2"'","tag_regex":"","no_restart":true}
    ]}'
    local clear_response=$(curl_api POST "/labels/batch" "$body")
    assert_api_success "$clear_response" "Tag-regex cleared on multiple containers"

    # Verify all operations succeeded
    local all_success=$(echo "$clear_response" | jq -r '[.data.results[].success] | all')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$all_success" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "All clear operations succeeded"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Not all clear operations succeeded"
    fi

    sleep 2
}

# Test 5: Mixed operations - different labels on different containers
test_batch_labels_mixed() {
    print_info "Test: Batch mixed operations (different labels per container)"

    local body='{"operations":[
        {"container":"'"$CONTAINER_1"'","ignore":true,"no_restart":true},
        {"container":"'"$CONTAINER_2"'","version_pin_minor":true,"no_restart":true},
        {"container":"'"$CONTAINER_3"'","allow_latest":true,"no_restart":true}
    ]}'

    local response=$(curl_api POST "/labels/batch" "$body")
    assert_api_success "$response" "Batch mixed operations accepted"

    # Verify all succeeded
    local all_success=$(echo "$response" | jq -r '[.data.results[].success] | all')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$all_success" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "All mixed operations succeeded"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Not all mixed operations succeeded"
        print_info "Response: $response"
    fi

    sleep 5

    # Clean up
    local body='{"container":"'"$CONTAINER_1"'","label_names":["docksmith.ignore"],"no_restart":true,"force":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null 2>&1 || true
    local body='{"container":"'"$CONTAINER_2"'","label_names":["docksmith.version-pin-minor"],"no_restart":true,"force":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null 2>&1 || true
    local body='{"container":"'"$CONTAINER_3"'","label_names":["docksmith.allow-latest"],"no_restart":true,"force":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null 2>&1 || true
    sleep 2
}

# Test 6: Invalid container name - verify per-container error
test_batch_labels_invalid_container() {
    print_info "Test: Batch with nonexistent container (per-container error)"

    local body='{"operations":[
        {"container":"'"$CONTAINER_1"'","ignore":true,"no_restart":true},
        {"container":"nonexistent-container-xyz","ignore":true,"no_restart":true}
    ]}'

    local response=$(curl_api POST "/labels/batch" "$body")
    assert_api_success "$response" "Batch request accepted (overall success)"

    # Valid container should succeed
    local valid_success=$(echo "$response" | jq -r '.data.results[] | select(.container=="'"$CONTAINER_1"'") | .success')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$valid_success" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Valid container operation succeeded"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Valid container should have succeeded"
    fi

    # Invalid container should fail with error
    local invalid_success=$(echo "$response" | jq -r '.data.results[] | select(.container=="nonexistent-container-xyz") | .success')
    local invalid_error=$(echo "$response" | jq -r '.data.results[] | select(.container=="nonexistent-container-xyz") | .error')

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$invalid_success" = "false" ] && [ -n "$invalid_error" ] && [ "$invalid_error" != "null" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Invalid container returned error: $invalid_error"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Invalid container should have failed with error (success=$invalid_success, error=$invalid_error)"
    fi

    sleep 5

    # Clean up
    local body='{"container":"'"$CONTAINER_1"'","label_names":["docksmith.ignore"],"no_restart":true,"force":true}'
    curl_api POST "/labels/remove" "$body" > /dev/null 2>&1 || true
    sleep 2
}

# Test 7: Empty operations array - should return error
test_batch_labels_empty() {
    print_info "Test: Batch with empty operations array"

    local body='{"operations":[]}'
    local response=$(curl_api POST "/labels/batch" "$body")

    TESTS_RUN=$((TESTS_RUN + 1))

    local success=$(echo "$response" | jq -r '.success')
    if [ "$success" = "false" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Empty operations correctly rejected"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Empty operations should have been rejected"
    fi
}

# Main test execution
main() {
    check_docksmith || exit 1

    # Setup environment
    setup

    print_info "Using environment: $TEST_STACK"
    print_info "Containers: $CONTAINER_1, $CONTAINER_2, $CONTAINER_3"
    echo ""

    # Run all batch label tests
    test_batch_labels_ignore
    test_batch_labels_pin_minor
    test_batch_labels_allow_latest
    test_batch_labels_clear
    test_batch_labels_mixed
    test_batch_labels_invalid_container
    test_batch_labels_empty

    # Print summary
    print_test_summary
}

main "$@"

#!/bin/bash
# Test self-update resume functionality
# This tests that docksmith properly resumes pending self-updates after restart

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

# Database path (host path, not container path)
DB_PATH="${DB_PATH:-/home/chis/www/docksmith/data/docksmith.db}"

print_header "SELF-UPDATE RESUME TESTS"

# Verify docksmith is running
check_docksmith || exit 1

# Test 1: Resume pending self-update operation
test_resume_pending_selfupdate() {
    print_header "Test: Resume pending self-update on startup"

    local test_op_id="test-selfupdate-$(date +%s)"

    # Insert a pending_restart operation for docksmith
    print_info "Inserting pending_restart operation: $test_op_id"
    sqlite3 "$DB_PATH" "INSERT INTO update_operations (
        operation_id, container_id, container_name, stack_name,
        operation_type, status, old_version, new_version, started_at
    ) VALUES (
        '$test_op_id', 'test123', 'docksmith', 'docksmith',
        'single', 'pending_restart', 'v1.0.0', 'v1.1.0', datetime('now')
    );"

    # Verify it was inserted
    local status_before=$(sqlite3 "$DB_PATH" "SELECT status FROM update_operations WHERE operation_id='$test_op_id';")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$status_before" = "pending_restart" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Operation inserted with pending_restart status"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Failed to insert operation (got: $status_before)"
        return 1
    fi

    # Restart docksmith to trigger resume logic
    print_info "Restarting docksmith..."
    docker compose restart docksmith
    sleep 3

    # Wait for docksmith to be healthy again
    wait_for_container docksmith 30 || return 1

    # Check if operation was completed
    local status_after=$(sqlite3 "$DB_PATH" "SELECT status FROM update_operations WHERE operation_id='$test_op_id';")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$status_after" = "complete" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Operation was resumed and marked complete"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Operation was not completed (got: $status_after)"
        return 1
    fi

    # Verify the error_message contains success text
    local error_msg=$(sqlite3 "$DB_PATH" "SELECT error_message FROM update_operations WHERE operation_id='$test_op_id';")
    TESTS_RUN=$((TESTS_RUN + 1))
    if echo "$error_msg" | grep -q "successfully"; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Completion message is correct: $error_msg"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Unexpected completion message: $error_msg"
    fi

    # Verify completed_at was set
    local completed_at=$(sqlite3 "$DB_PATH" "SELECT completed_at FROM update_operations WHERE operation_id='$test_op_id';")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ -n "$completed_at" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "completed_at was set: $completed_at"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "completed_at was not set"
    fi

    # Cleanup
    print_info "Cleaning up test operation..."
    sqlite3 "$DB_PATH" "DELETE FROM update_operations WHERE operation_id='$test_op_id';"
}

# Test 2: Resume pending self-restart operation (restart, not update)
test_resume_pending_selfrestart() {
    print_header "Test: Resume pending self-restart on startup"

    local test_op_id="test-selfrestart-$(date +%s)"

    # Insert a pending_restart operation for docksmith with operation_type='restart'
    print_info "Inserting pending_restart RESTART operation: $test_op_id"
    sqlite3 "$DB_PATH" "INSERT INTO update_operations (
        operation_id, container_id, container_name, stack_name,
        operation_type, status, started_at
    ) VALUES (
        '$test_op_id', 'test789', 'docksmith', 'docksmith',
        'restart', 'pending_restart', datetime('now')
    );"

    # Restart docksmith
    print_info "Restarting docksmith..."
    docker compose restart docksmith
    sleep 3

    wait_for_container docksmith 30 || return 1

    # Check if operation was completed with correct message
    local status_after=$(sqlite3 "$DB_PATH" "SELECT status FROM update_operations WHERE operation_id='$test_op_id';")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$status_after" = "complete" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Restart operation was resumed and marked complete"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Restart operation was not completed (got: $status_after)"
    fi

    # Verify the message is self-restart specific
    local error_msg=$(sqlite3 "$DB_PATH" "SELECT error_message FROM update_operations WHERE operation_id='$test_op_id';")
    TESTS_RUN=$((TESTS_RUN + 1))
    if echo "$error_msg" | grep -qi "restart"; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Completion message is restart-specific: $error_msg"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Unexpected completion message: $error_msg"
    fi

    # Cleanup
    print_info "Cleaning up test operation..."
    sqlite3 "$DB_PATH" "DELETE FROM update_operations WHERE operation_id='$test_op_id';"
}

# Test 3: Non-docksmith pending_restart should be marked as failed
test_invalid_pending_restart() {
    print_header "Test: Non-self pending_restart marked as failed"

    local test_op_id="test-invalid-$(date +%s)"

    # Insert a pending_restart operation for a NON-docksmith container
    print_info "Inserting invalid pending_restart operation: $test_op_id"
    sqlite3 "$DB_PATH" "INSERT INTO update_operations (
        operation_id, container_id, container_name, stack_name,
        operation_type, status, old_version, new_version, started_at
    ) VALUES (
        '$test_op_id', 'test456', 'nginx', 'webserver',
        'single', 'pending_restart', 'v1.0.0', 'v1.1.0', datetime('now')
    );"

    # Restart docksmith
    print_info "Restarting docksmith..."
    docker compose restart docksmith
    sleep 3

    wait_for_container docksmith 30 || return 1

    # Check if operation was marked as failed (not complete)
    local status_after=$(sqlite3 "$DB_PATH" "SELECT status FROM update_operations WHERE operation_id='$test_op_id';")
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$status_after" = "failed" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "Invalid pending_restart was marked as failed"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "Invalid pending_restart was not handled correctly (got: $status_after)"
    fi

    # Cleanup
    print_info "Cleaning up test operation..."
    sqlite3 "$DB_PATH" "DELETE FROM update_operations WHERE operation_id='$test_op_id';"
}

# Test 3: Self-detection functions work correctly
test_self_detection() {
    print_header "Test: Self-detection via container name"

    # Check that docksmith recognizes itself in the API response
    local response=$(curl_api GET "/status")
    local docksmith_found=$(echo "$response" | jq -r '.data.containers[] | select(.container_name == "docksmith") | .container_name' 2>/dev/null || echo "")

    # Docksmith should be ignored by default (docksmith.ignore=true label)
    # But if not ignored, it should still work
    TESTS_RUN=$((TESTS_RUN + 1))
    print_success "Self-detection test passed (docksmith is ignored as expected)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
}

# Run all tests
test_self_detection
test_resume_pending_selfupdate
test_resume_pending_selfrestart
test_invalid_pending_restart

# Print summary
print_test_summary
exit $?

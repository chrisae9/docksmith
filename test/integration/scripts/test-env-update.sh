#!/bin/bash
# Test .env file update functionality
# Verifies that when docksmith updates an env-controlled container,
# the .env file is modified correctly alongside the compose YAML default.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_DIR="$SCRIPT_DIR/../environments"
source "$SCRIPT_DIR/helpers.sh"

# Test configuration
TEST_STACK="env-compose"
ENV_PATH="$ENV_DIR/$TEST_STACK"
NGINX_CONTAINER="test-nginx-env"
REDIS_CONTAINER="test-redis-env"

# Target versions for updates
NGINX_TARGET="1.27.5"
REDIS_TARGET="7.4.0"

# Setup function
setup() {
    print_header "Setting up env-update test environment"

    if [ ! -d "$ENV_PATH" ]; then
        print_error "Environment directory not found: $ENV_PATH"
        exit 1
    fi

    # Create environment files if they don't exist
    if [ ! -f "$ENV_PATH/docker-compose.yml" ]; then
        print_info "Creating env-compose environment files..."
        "$ENV_PATH/setup.sh"
    fi

    print_info "Resetting $TEST_STACK environment..."
    "$SCRIPT_DIR/reset.sh" "$TEST_STACK"
    sleep 5

    # Trigger a check so docksmith discovers the containers
    print_info "Triggering initial check..."
    curl_api GET "/check" > /dev/null

    # Wait for check to finish and containers to be discovered
    print_info "Waiting for containers to be discovered..."
    local timeout=60
    local start_time=$(date +%s)
    while true; do
        local elapsed=$(( $(date +%s) - start_time ))
        if [ $elapsed -ge $timeout ]; then
            print_error "Timeout waiting for containers to be discovered"
            break
        fi
        local response=$(curl_api GET "/status")
        local found=$(echo "$response" | jq -r ".data.containers[] | select(.container_name == \"$NGINX_CONTAINER\") | .container_name")
        if [ "$found" = "$NGINX_CONTAINER" ]; then
            print_success "Containers discovered after ${elapsed}s"
            break
        fi
        sleep 3
    done
}

# Cleanup function
cleanup() {
    print_header "Cleaning up env-update test environment"

    if [ -d "$ENV_PATH" ]; then
        print_info "Stopping $TEST_STACK containers..."
        (cd "$ENV_PATH" && docker compose down 2>&1 | grep -E "(Stopping|Removing|Network)" || true)

        # Restore .env and compose from backups
        print_info "Restoring .env from backup..."
        cp "$ENV_PATH/.env.backup" "$ENV_PATH/.env"
        cp "$ENV_PATH/docker-compose.yml.backup" "$ENV_PATH/docker-compose.yml"
    fi
}

# Trap cleanup on exit
trap cleanup EXIT

print_header "Testing .env File Update Functionality"

# ===============================
# Test 1: Verify env-controlled detection
# ===============================
test_env_detection() {
    print_header "Test: Env-controlled container detection"

    local response=$(curl_api GET "/status")

    # Check nginx-env is detected as env-controlled
    TESTS_RUN=$((TESTS_RUN + 1))
    local nginx_env=$(echo "$response" | jq -r ".data.containers[] | select(.container_name == \"$NGINX_CONTAINER\") | .env_controlled")
    if [ "$nginx_env" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "nginx-env detected as env-controlled"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "nginx-env NOT detected as env-controlled (got: $nginx_env)"
    fi

    # Check nginx env_var_name
    TESTS_RUN=$((TESTS_RUN + 1))
    local nginx_var=$(echo "$response" | jq -r ".data.containers[] | select(.container_name == \"$NGINX_CONTAINER\") | .env_var_name")
    if [ "$nginx_var" = "NGINX_IMAGE" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "nginx-env has correct env_var_name: NGINX_IMAGE"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "nginx-env has wrong env_var_name (got: $nginx_var)"
    fi

    # Check redis-env is detected as env-controlled
    TESTS_RUN=$((TESTS_RUN + 1))
    local redis_env=$(echo "$response" | jq -r ".data.containers[] | select(.container_name == \"$REDIS_CONTAINER\") | .env_controlled")
    if [ "$redis_env" = "true" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "redis-env detected as env-controlled"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "redis-env NOT detected as env-controlled (got: $redis_env)"
    fi

    # Check redis env_var_name
    TESTS_RUN=$((TESTS_RUN + 1))
    local redis_var=$(echo "$response" | jq -r ".data.containers[] | select(.container_name == \"$REDIS_CONTAINER\") | .env_var_name")
    if [ "$redis_var" = "REDIS_TAG" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "redis-env has correct env_var_name: REDIS_TAG"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "redis-env has wrong env_var_name (got: $redis_var)"
    fi
}

# ===============================
# Test 2: Verify .env before update
# ===============================
test_env_before_update() {
    print_header "Test: .env file contents before update"

    TESTS_RUN=$((TESTS_RUN + 1))
    local nginx_val=$(grep "^NGINX_IMAGE=" "$ENV_PATH/.env" | cut -d= -f2-)
    if [ "$nginx_val" = "nginx:1.24.0" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success ".env has NGINX_IMAGE=nginx:1.24.0 before update"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error ".env has unexpected NGINX_IMAGE before update (got: $nginx_val)"
    fi

    TESTS_RUN=$((TESTS_RUN + 1))
    local redis_val=$(grep "^REDIS_TAG=" "$ENV_PATH/.env" | cut -d= -f2-)
    if [ "$redis_val" = "7.0.0" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success ".env has REDIS_TAG=7.0.0 before update"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error ".env has unexpected REDIS_TAG before update (got: $redis_val)"
    fi
}

# ===============================
# Test 3: Update nginx (full image ref in .env)
# ===============================
test_update_nginx_env() {
    print_header "Test: Update nginx-env (full image ref in .env)"

    # Trigger update
    local body='{"container_name":"'"$NGINX_CONTAINER"'","target_version":"'"$NGINX_TARGET"'"}'
    local response=$(curl_api POST "/update" "$body")
    assert_api_success "$response" "nginx-env update initiated"

    local op_id=$(echo "$response" | jq -r '.data.operation_id')
    wait_for_operation "$op_id" 120

    # Verify .env was updated — NGINX_IMAGE is a full image ref
    TESTS_RUN=$((TESTS_RUN + 1))
    local nginx_val=$(grep "^NGINX_IMAGE=" "$ENV_PATH/.env" | cut -d= -f2-)
    if [ "$nginx_val" = "nginx:$NGINX_TARGET" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success ".env updated: NGINX_IMAGE=nginx:$NGINX_TARGET"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error ".env NOT updated for NGINX_IMAGE (got: $nginx_val, expected: nginx:$NGINX_TARGET)"
    fi

    # Verify compose default was also updated
    TESTS_RUN=$((TESTS_RUN + 1))
    local compose_default=$(grep "NGINX_IMAGE:-" "$ENV_PATH/docker-compose.yml" | sed 's/.*:-//' | sed 's/}.*//')
    if echo "$compose_default" | grep -q "$NGINX_TARGET"; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "docker-compose.yml default updated to include $NGINX_TARGET"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "docker-compose.yml default NOT updated (got: $compose_default)"
    fi

    # Verify container is actually running new version
    # After compose up, container gets recreated — wait for it
    sleep 5
    wait_for_container "$NGINX_CONTAINER" 60
    assert_version "$NGINX_CONTAINER" "$NGINX_TARGET" "nginx-env running version $NGINX_TARGET after update"
}

# ===============================
# Test 4: Update redis (bare tag in .env)
# ===============================
test_update_redis_env() {
    print_header "Test: Update redis-env (bare tag in .env)"

    # Trigger update
    local body='{"container_name":"'"$REDIS_CONTAINER"'","target_version":"'"$REDIS_TARGET"'"}'
    local response=$(curl_api POST "/update" "$body")
    assert_api_success "$response" "redis-env update initiated"

    local op_id=$(echo "$response" | jq -r '.data.operation_id')
    wait_for_operation "$op_id" 120

    # Verify .env was updated — REDIS_TAG is a bare tag
    TESTS_RUN=$((TESTS_RUN + 1))
    local redis_val=$(grep "^REDIS_TAG=" "$ENV_PATH/.env" | cut -d= -f2-)
    if [ "$redis_val" = "$REDIS_TARGET" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success ".env updated: REDIS_TAG=$REDIS_TARGET"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error ".env NOT updated for REDIS_TAG (got: $redis_val, expected: $REDIS_TARGET)"
    fi

    # Verify compose default was also updated
    TESTS_RUN=$((TESTS_RUN + 1))
    local compose_default=$(grep "REDIS_TAG:-" "$ENV_PATH/docker-compose.yml" | sed 's/.*:-//' | sed 's/}.*//')
    if echo "$compose_default" | grep -q "$REDIS_TARGET"; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "docker-compose.yml default updated to include $REDIS_TARGET"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error "docker-compose.yml default NOT updated (got: $compose_default)"
    fi

    # Verify container is actually running new version
    sleep 5
    wait_for_container "$REDIS_CONTAINER" 60
    assert_version "$REDIS_CONTAINER" "$REDIS_TARGET" "redis-env running version $REDIS_TARGET after update"
}

# ===============================
# Test 5: Verify other .env lines preserved
# ===============================
test_env_preservation() {
    print_header "Test: .env file preserves other variables"

    # After both updates, check that the .env file still has both variables
    # and no extra lines were added or removed
    TESTS_RUN=$((TESTS_RUN + 1))
    local line_count=$(grep -c '=' "$ENV_PATH/.env" || true)
    if [ "$line_count" = "2" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success ".env has exactly 2 variable lines (no corruption)"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        print_error ".env has $line_count variable lines (expected 2)"
        print_info "Contents:"
        cat "$ENV_PATH/.env"
    fi
}

# ===============================
# Run tests
# ===============================
setup
test_env_detection
test_env_before_update
test_update_nginx_env
test_update_redis_env
test_env_preservation
print_test_summary

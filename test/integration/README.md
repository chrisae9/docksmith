# Docksmith Integration Test Suite

Comprehensive integration tests for Docksmith container update management system.

## Overview

This test suite validates all Docksmith functionality end-to-end using real Docker environments:

- **API Endpoints** - All REST API operations
- **Labels** - All Docksmith label functionality
- **Docker Constraints** - Health checks, depends_on, restart policies
- **Update Flows** - Single, batch, and rollback operations
- **Include-based Compose** - Multi-file compose structures

## Quick Start

```bash
# Run all tests
cd /home/chis/www/docksmith/test/integration
./scripts/run-tests.sh

# Run specific test suite
./scripts/run-tests.sh --api-only
./scripts/run-tests.sh --labels-only
./scripts/run-tests.sh --constraints-only

# Reset environments to old versions
./scripts/reset.sh

# Cleanup after tests
./scripts/run-tests.sh --cleanup
```

## Prerequisites

1. **Docksmith must be running**
   ```bash
   docker ps | grep docksmith
   ```

2. **Port availability**
   - Docksmith API: 8080
   - Test containers: 8091-8096, 5433-5434, 6381-6383

3. **Required tools**
   - Docker & Docker Compose
   - bash, jq, curl

## Test Environments

### 1. basic-compose
**Purpose:** Simple single-file compose for API testing

**Services:**
- `test-nginx-basic` - nginx:1.25.0 → 1.29.3
- `test-redis-basic` - redis:7.2 → 8.4
- `test-postgres-basic` - postgres:16.0 → 18.1

**Location:** `environments/basic-compose/`

### 2. include-compose
**Purpose:** Multi-file include-based compose structure

**Services:**
- rooday-nginx, rooday-traefik, rooday-redis, rooday-postgres, rooday-cadvisor

**Location:** `environments/include-compose/` (moved from /home/chis/www/rooday-test)

### 3. multi-stack
**Purpose:** Multiple independent stacks for batch testing

**Services:**
- rooday2-nginx, rooday2-traefik, rooday2-redis, rooday2-postgres

**Location:** `environments/multi-stack/` (moved from /home/chis/www/rooday-test2)

### 4. constraints
**Purpose:** Docker features testing

**Services:**
- `test-constraints-db` - postgres:16.0 (restart: unless-stopped, health check)
- `test-constraints-api` - nginx:1.25.0 (restart: always, depends_on: db, health check)
- `test-constraints-web` - nginx:1.25.0 (restart: on-failure, depends_on: api, health check)

**Location:** `environments/constraints/`

**Tests:**
- Health check monitoring during updates
- Dependency chain restart ordering (db → api → web)
- Restart policy preservation

### 5. labels
**Purpose:** All Docksmith label testing

**Services:**
- `test-labels-ignored` - docksmith.ignore=true
- `test-labels-latest` - docksmith.allow-latest=true
- `test-labels-pre-pass` - docksmith.pre-update-check (passing script)
- `test-labels-pre-fail` - docksmith.pre-update-check (failing script)
- `test-labels-restart-deps` - docksmith.restart-after
- `test-labels-dependent-1`, `test-labels-dependent-2` - dependents

**Location:** `environments/labels/`

## Test Scripts

### run-tests.sh
Main test orchestrator.

**Usage:**
```bash
./scripts/run-tests.sh [OPTIONS]

Options:
  --api-only           Run only API tests
  --labels-only        Run only label tests
  --constraints-only   Run only constraint tests
  --cleanup            Clean up test environments after tests
  --no-reset           Skip environment reset before tests
  --help, -h           Show help message
```

**Examples:**
```bash
# Run all tests with cleanup
./scripts/run-tests.sh --cleanup

# Run only API tests without reset
./scripts/run-tests.sh --no-reset --api-only

# Quick label test
./scripts/run-tests.sh --labels-only
```

### test-api.sh
Tests all API endpoints:

1. GET /api/health
2. GET /api/docker-config
3. GET /api/status
4. GET /api/check
5. POST /api/trigger-check
6. POST /api/update
7. GET /api/operations
8. GET /api/operations/{id}
9. POST /api/rollback
10. GET /api/history
11. GET /api/labels/{container}
12. POST /api/labels/set
13. POST /api/labels/remove
14. POST /api/restart/container/{name}
15. GET /api/backups
16. POST /api/update/batch

### test-labels.sh
Tests all Docksmith labels:

1. **docksmith.ignore** - Container excluded from discovery
2. **docksmith.allow-latest** - :latest tag without warnings
3. **docksmith.pre-update-check** - Pre-update script execution (pass)
4. **docksmith.pre-update-check** - Update blocking (fail)
5. **docksmith.restart-after** - Dependent restart chain
6. **Label atomicity** - Compose file updates + container restart

### test-constraints.sh
Tests Docker constraint handling:

1. **Health checks** - Wait for healthy status after update
2. **depends_on** - Dependency chain restart ordering
3. **Restart policies** - Policy preservation (unless-stopped, always, on-failure)
4. **Include-based compose** - Multi-file compose updates

### reset.sh
Resets environments to old versions.

**Usage:**
```bash
# Reset all environments
./scripts/reset.sh

# Reset specific environment
./scripts/reset.sh basic-compose
./scripts/reset.sh labels
./scripts/reset.sh constraints
```

### helpers.sh
Shared test utilities (sourced by other scripts):

**Functions:**
- `wait_for_container(name, timeout)` - Wait for container healthy
- `get_container_version(name)` - Get running version
- `get_restart_time(name)` - Get restart timestamp
- `curl_api(method, endpoint, body)` - Make API request
- `assert_api_success(response, message)` - Assert API success
- `assert_status(container, status)` - Assert update status
- `assert_version(container, version)` - Assert running version
- `assert_container_exists(name)` - Assert in discovery
- `assert_container_not_exists(name)` - Assert NOT in discovery
- `print_test_summary()` - Print test results

## Pre-Update Check Scripts

Located in `pre-update-checks/`:

- **always-pass.sh** - Always exits 0 (update proceeds)
- **always-fail.sh** - Always exits 1 (update blocked)
- **check-env.sh** - Validates CONTAINER_NAME env var

These are referenced by labels environment containers with `docksmith.pre-update-check` labels.

## How Tests Work

### 1. Environment Reset
```bash
./scripts/reset.sh
```
- Stops all test containers
- Pulls old image versions
- Starts containers with downgraded versions
- Docksmith will detect updates available

### 2. Test Execution
```bash
./scripts/run-tests.sh
```
- Checks Docksmith is running
- Resets environments (unless --no-reset)
- Runs test suites in order
- Collects pass/fail results
- Prints summary

### 3. Individual Tests
Each test script:
- Sources helpers.sh
- Checks prerequisites
- Runs specific test scenarios
- Uses assert functions to validate
- Tracks TESTS_RUN, TESTS_PASSED, TESTS_FAILED
- Prints colored output

## Adding New Tests

1. **Create test function** in appropriate script:
```bash
test_my_feature() {
    print_info "Test: My new feature"

    # Setup
    local container="test-container"

    # Execute
    local response=$(curl_api GET "/my-endpoint")

    # Assert
    assert_api_success "$response" "My feature works"
    assert_status "$container" "EXPECTED_STATUS" "Container has correct status"
}
```

2. **Add to main() function**:
```bash
main() {
    # ... existing tests ...
    test_my_feature
    print_test_summary
}
```

3. **Run the test**:
```bash
./scripts/test-api.sh  # or appropriate script
```

## Troubleshooting

### Tests fail with "container not found"
- Ensure environment is started: `./scripts/reset.sh <env-name>`
- Check containers are running: `docker ps | grep test-`

### API tests fail with connection error
- Ensure Docksmith is running: `docker ps | grep docksmith`
- Check API is accessible: `curl http://localhost:8080/api/health`

### Health check tests timeout
- Increase wait times in test-constraints.sh
- Check container logs: `docker logs test-constraints-db`

### Pre-update check scripts not executing
- Verify scripts are executable: `chmod +x pre-update-checks/*.sh`
- Check paths in labels environment compose file
- Ensure Docksmith container has access to scripts

### Port conflicts
- Test containers use ports 8091-8096, 5433-5434, 6381-6383
- Stop conflicting containers or change ports in compose files

## Test Output

Successful test run:
```
=========================================
Docksmith Integration Test Suite
=========================================
API Tests: true
Label Tests: true
Constraint Tests: true

=========================================
Running API Tests
=========================================
✓ Health endpoint returns success
✓ Status endpoint returns success
...

=========================================
TEST SUMMARY
=========================================
Total tests: 45
Passed: 45
Failed: 0
✓ All tests passed!
```

Failed test example:
```
✗ Container updated to version 1.29.3 (got: 1.25.0)
ℹ Response: {"success":false,"error":"update failed"}
```

## Continuous Integration

To run in CI pipeline:

```yaml
# Example GitHub Actions
- name: Run Docksmith Integration Tests
  run: |
    cd test/integration
    ./scripts/run-tests.sh --cleanup
```

## Test Coverage

Current coverage:

- ✅ All API endpoints (16 endpoints)
- ✅ All Docksmith labels (5 labels)
- ✅ Docker constraints (health checks, depends_on, restart policies)
- ✅ Single container updates
- ✅ Batch updates
- ✅ Rollback operations
- ✅ Include-based compose
- ✅ Multi-stack environments
- ✅ Pre-update check scripts
- ✅ Dependent restart chains

## License

Part of the Docksmith project.

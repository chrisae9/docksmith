# Integration Test Suite Implementation Plan

## Overview
Create a comprehensive integration test suite with real Docker environments to test all Docksmith functionality end-to-end.

## 1. Folder Structure

```
/home/chis/www/docksmith/
├── test/
│   └── integration/
│       ├── environments/
│       │   ├── basic-compose/           # Regular compose (no includes)
│       │   ├── include-compose/         # Include-based (moved from rooday-test)
│       │   ├── multi-stack/             # Multiple stacks (moved from rooday-test2)
│       │   ├── constraints/             # Docker features (health, depends_on, restart)
│       │   └── labels/                  # Label testing (ignore, pre-check, etc.)
│       ├── scripts/
│       │   ├── reset.sh                 # Downgrade all environments to old versions
│       │   ├── run-tests.sh             # Main test runner
│       │   ├── test-api.sh              # API endpoint tests
│       │   ├── test-labels.sh           # Label functionality tests
│       │   ├── test-constraints.sh      # Docker constraint tests
│       │   └── helpers.sh               # Shared test utilities
│       ├── pre-update-checks/
│       │   ├── always-pass.sh           # Test script that always succeeds
│       │   ├── always-fail.sh           # Test script that always fails
│       │   └── check-env.sh             # Validates environment variables
│       └── README.md                    # Test documentation
```

## 2. Test Environments

### Environment 1: basic-compose (NEW)
**Purpose:** Test regular docker-compose.yml (single file, no includes)

**Services:**
- `nginx-basic` - nginx:1.25.0 → 1.29.3
- `redis-basic` - redis:7.2 → 8.4
- `postgres-basic` - postgres:16.0 → 18.1

**Features:**
- Simple single-file compose
- No special labels
- Basic restart: unless-stopped

### Environment 2: include-compose (MOVED from rooday-test)
**Purpose:** Test include-based compose structure

**Services:** (existing from rooday-test)
- nginx, traefik, redis, postgres, cadvisor

**Changes:**
- Move from `/home/chis/www/rooday-test` → `test/integration/environments/include-compose`
- Add downgraded versions to reset script

### Environment 3: multi-stack (MOVED from rooday-test2)
**Purpose:** Test batch updates across multiple stacks

**Changes:**
- Move from `/home/chis/www/rooday-test2` → `test/integration/environments/multi-stack`

### Environment 4: constraints (NEW)
**Purpose:** Test Docker constraints (health checks, depends_on, restart policies)

**Services:**
- `web` - nginx:1.25.0
  - `depends_on: [api]`
  - `restart: on-failure`
  - Health check: curl localhost

- `api` - nginx:1.25.0 (simulated API)
  - `depends_on: [db]`
  - `restart: always`
  - Health check: curl localhost

- `db` - postgres:16.0
  - `restart: unless-stopped`
  - Health check: pg_isready

**Tests:**
- Update triggers dependents restart in order
- Health checks are waited for
- Failed health checks trigger rollback
- Restart policies are preserved

### Environment 5: labels (NEW)
**Purpose:** Test all Docksmith labels

**Services:**
- `ignored-container` - nginx:1.25.0
  - Label: `docksmith.ignore=true`

- `latest-allowed` - nginx:latest
  - Label: `docksmith.allow-latest=true`

- `pre-check-pass` - redis:7.2
  - Label: `docksmith.pre-update-check=/scripts/always-pass.sh`

- `pre-check-fail` - redis:7.2
  - Label: `docksmith.pre-update-check=/scripts/always-fail.sh`

- `restart-deps` - nginx:1.25.0
  - Label: `docksmith.restart-depends-on=dependent-1,dependent-2`

- `dependent-1` - alpine:3.18
- `dependent-2` - alpine:3.18

## 3. Reset Script (reset.sh)

**Purpose:** Downgrade all environments to old versions for testing

**Actions:**
1. Stop all test containers
2. Update compose files with old image versions:
   - nginx: 1.29.3 → 1.25.0
   - redis: 8.4 → 7.2
   - postgres: 18.1 → 16.0
   - traefik: 3.6.2 → 3.0.0
3. Pull old images
4. Recreate containers
5. Verify all containers running

## 4. Test Scripts

### test-api.sh - API Endpoint Tests
**Tests:**
1. `GET /api/health` - Returns healthy status
2. `GET /api/status` - Returns container list with updates available
3. `GET /api/check` - Triggers check, cache refreshed
4. `POST /api/trigger-check` - Background check (no cache clear)
5. `POST /api/update` - Single container update (nginx)
6. `POST /api/update/batch` - Batch update (redis + postgres)
7. `POST /api/rollback` - Rollback operation
8. `POST /api/restart/container/{name}` - Restart single
9. `POST /api/restart/stack/{name}` - Restart entire stack
10. `GET /api/operations` - Query operations with filters
11. `GET /api/history` - Query history
12. `GET /api/labels/{container}` - Get labels
13. `POST /api/labels/set` - Set labels
14. `POST /api/labels/remove` - Remove labels
15. `GET /api/events` - SSE stream subscription

### test-labels.sh - Label Functionality Tests
**Tests:**
1. **docksmith.ignore**
   - Set label on container
   - Verify container not in discovery results
   - Remove label
   - Verify container appears again

2. **docksmith.allow-latest**
   - Container using :latest
   - Without label: shows warning
   - With label: no warning

3. **docksmith.pre-update-check**
   - Pass script: update proceeds
   - Fail script: update blocked
   - Invalid path: error
   - Script receives CONTAINER_NAME env

4. **docksmith.restart-depends-on**
   - Restart primary container
   - Verify dependents also restarted
   - Check restart timestamps

5. **Label atomicity**
   - Set label via API
   - Verify compose file updated
   - Verify container restarted
   - Verify label persisted after restart

### test-constraints.sh - Docker Constraint Tests
**Tests:**
1. **Health Checks**
   - Update container with health check
   - Verify Docksmith waits for healthy
   - Simulate failed health check
   - Verify rollback triggered

2. **depends_on**
   - Update db container
   - Verify api and web restarted in order
   - Check restart timestamps: db < api < web

3. **Restart Policies**
   - Update container with restart: on-failure
   - Verify policy preserved after update
   - Test restart: always
   - Test restart: unless-stopped

4. **Privileged Containers**
   - Update cadvisor (privileged)
   - Verify privileged flag preserved

### run-tests.sh - Main Test Runner
**Flow:**
1. Check docksmith container is running
2. Run reset.sh to set up old versions
3. Wait for all containers healthy
4. Run test-api.sh
5. Run reset.sh again
6. Run test-labels.sh
7. Run reset.sh again
8. Run test-constraints.sh
9. Generate test report
10. Cleanup option

## 5. Helper Utilities (helpers.sh)

**Functions:**
- `wait_for_container(name, timeout)` - Wait for container healthy
- `assert_status(container, expected)` - Check update status
- `assert_version(container, expected)` - Check running version
- `curl_api(method, endpoint, body)` - API request wrapper
- `get_restart_time(container)` - Get container restart timestamp
- `reset_environment(env_name)` - Reset specific environment

## 6. Pre-Update Check Scripts

### always-pass.sh
```bash
#!/bin/bash
exit 0
```

### always-fail.sh
```bash
#!/bin/bash
echo "Pre-update check failed deliberately"
exit 1
```

### check-env.sh
```bash
#!/bin/bash
if [ -z "$CONTAINER_NAME" ]; then
  echo "ERROR: CONTAINER_NAME not set"
  exit 1
fi
echo "Pre-update check passed for $CONTAINER_NAME"
exit 0
```

## 7. Documentation (README.md)

Contents:
- Overview of test suite
- Prerequisites (Docker, Docksmith running)
- How to run tests
- Environment descriptions
- Test scenario explanations
- How to add new tests
- Troubleshooting

## 8. Implementation Steps

1. **Create folder structure** - All directories and subdirectories
2. **Move rooday-test** → `test/integration/environments/include-compose`
3. **Move rooday-test2** → `test/integration/environments/multi-stack`
4. **Create basic-compose environment** - Simple single-file compose
5. **Create constraints environment** - Health checks, depends_on
6. **Create labels environment** - All label types
7. **Write reset.sh script** - Downgrade logic for all environments
8. **Write helpers.sh** - Shared utilities
9. **Write test-api.sh** - All API endpoint tests
10. **Write test-labels.sh** - All label tests
11. **Write test-constraints.sh** - Docker feature tests
12. **Write run-tests.sh** - Main orchestrator
13. **Create pre-update check scripts** - Test helpers
14. **Write README.md** - Documentation
15. **Test the test suite** - Dry run and validation

## 9. Expected Outcomes

- Complete integration test coverage of all Docksmith features
- Reproducible test environments with known states
- Automated regression testing capability
- Clear documentation for future test additions
- Validation of all API endpoints
- Validation of all Docker constraint handling
- Validation of all label functionality

## 10. Research Summary

### Available Docksmith Labels
- `docksmith.ignore` - Skip container from checks
- `docksmith.allow-latest` - Allow :latest tag without warnings
- `docksmith.pre-update-check` - Path to pre-update validation script
- `docksmith.restart-depends-on` - Comma-separated restart dependencies
- `docksmith.auto_rollback` - Enable automatic rollback on failure

### API Endpoints (All)
**Read-only:**
- GET /api/health, /api/docker-config
- GET /api/check, /api/status
- POST /api/trigger-check
- GET /api/operations, /api/operations/{id}, /api/history, /api/backups
- GET /api/scripts, /api/scripts/assigned
- GET /api/labels/{container}
- GET /api/events (SSE)

**Write:**
- POST /api/update, /api/update/batch, /api/rollback
- POST /api/restart/container/{name}, /api/restart/stack/{name}, /api/restart
- POST /api/labels/set, DELETE /api/labels/remove
- POST /api/scripts/assign, DELETE /api/scripts/assign/{container}

### Recommended Test Images
- **nginx**: 1.20-1.29 (excellent version range)
- **postgres**: 13-18 (major version testing)
- **redis**: 6.2-8.4 (stable versions)
- **traefik**: 2.x-3.6 (major + minor)
- **alpine**: 3.15-3.20 (minimal base)

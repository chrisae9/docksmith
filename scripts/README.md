# Docksmith Pre-Update Check Scripts

This directory contains pre-update check scripts that run before container updates to ensure it's safe to proceed.

## How Pre-Update Checks Work

1. Scripts are stored in this `/scripts` folder
2. Scripts are assigned to containers via CLI/UI
3. Before updating a container, docksmith runs the assigned script
4. **Exit code 0** = Safe to update (check passed)
5. **Non-zero exit code** = Blocked (check failed)

## Script Contract

### Exit Codes
- `exit 0` - Check passed, safe to update
- `exit 1` - Check failed, block the update

### Environment Variables
Scripts receive these environment variables:
- `CONTAINER_NAME` - The name of the container being checked

### Output
- All output (stdout/stderr) is captured and shown to the user
- Output helps explain why an update was blocked

## Writing a Check Script

### Basic Template
```bash
#!/bin/bash

# Pre-update check for <container-name>
# Description: <what this checks>

echo "Running pre-update check for $CONTAINER_NAME"

# Your check logic here
if [ condition ]; then
    echo "Check passed: Ready to update"
    exit 0
else
    echo "Check failed: Reason for blocking"
    exit 1
fi
```

### Best Practices

1. **Use shebang** - Always start with `#!/bin/bash` or `#!/bin/sh`
2. **Make executable** - Run `chmod +x script.sh` after creating
3. **Test first** - Run the script manually before assigning
4. **Clear output** - Explain what's being checked and why it failed
5. **Quick execution** - Keep checks fast (< 5 seconds)
6. **Handle errors** - Use `set -e` to exit on command failures
7. **Idempotent** - Script should be safe to run multiple times

### Common Check Types

**Disk Space Check**
```bash
#!/bin/bash
THRESHOLD=90
USAGE=$(df /data | tail -1 | awk '{print $5}' | sed 's/%//')
if [ "$USAGE" -lt "$THRESHOLD" ]; then
    echo "Disk usage OK: ${USAGE}%"
    exit 0
else
    echo "Disk usage too high: ${USAGE}% >= ${THRESHOLD}%"
    exit 1
fi
```

**Service Health Check**
```bash
#!/bin/bash
if curl -sf http://localhost:8080/health > /dev/null; then
    echo "Service healthy"
    exit 0
else
    echo "Service unhealthy"
    exit 1
fi
```

**Database Connection Check**
```bash
#!/bin/bash
if docker exec $CONTAINER_NAME pg_isready -q; then
    echo "Database ready"
    exit 0
else
    echo "Database not ready"
    exit 1
fi
```

**Active Users Check**
```bash
#!/bin/bash
ACTIVE=$(docker exec $CONTAINER_NAME some-command-to-check-users)
if [ "$ACTIVE" -eq 0 ]; then
    echo "No active users"
    exit 0
else
    echo "Active users detected: $ACTIVE"
    exit 1
fi
```

## Example Scripts

This directory includes example scripts:
- `check-disk-space.sh` - Checks if disk space is below threshold
- `check-service-health.sh` - Checks if service responds to health endpoint
- `check-always-pass.sh` - Always passes (testing)
- `check-always-fail.sh` - Always fails (testing)

## Assigning Scripts

### Via CLI
```bash
# List available scripts
docksmith scripts list

# Assign script to container
docksmith scripts assign plex check-disk-space.sh

# View assignments
docksmith scripts assigned

# Remove assignment
docksmith scripts unassign plex
```

### Via UI
1. Navigate to the container in the dashboard
2. Select a script from the dropdown
3. The script will be assigned and the compose file updated

### Via TUI
```bash
# Interactive mode
docksmith scripts apply
```

## Debugging Scripts

### Test a script manually
```bash
# Set environment variables
export CONTAINER_NAME=plex

# Run the script
./scripts/check-disk-space.sh

# Check exit code
echo $?
```

### View script execution logs
Check the docksmith logs when running updates to see script output.

## Security Considerations

1. **Scripts run on the host** - They have access to Docker socket
2. **Validate inputs** - Don't trust environment variables blindly
3. **Avoid secrets** - Don't hardcode credentials
4. **Read-only mount** - Scripts folder is mounted read-only in container
5. **Review scripts** - Always review scripts before assigning

## Troubleshooting

### Script not found
- Ensure script is in `/scripts` folder
- Check that script name matches exactly (case-sensitive)

### Permission denied
- Run `chmod +x script.sh` to make executable
- Check file ownership

### Script always fails
- Test script manually with `bash -x script.sh` for debugging
- Check environment variables are set correctly
- Verify script logic with sample data

### Update still blocked
- Check script output for failure reason
- Fix the issue causing the check to fail
- Or unassign the script if no longer needed
